package keeper

import (
	"errors"

	"interchange-nel/x/dex/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	host "github.com/cosmos/ibc-go/v3/modules/core/24-host"
)

// TransmitSellOrderPacket transmits the packet over IBC with the specified source port and source
// channel
func (k Keeper) TransmitSellOrderPacket(
	ctx sdk.Context,
	packetData types.SellOrderPacketData,
	sourcePort,
	sourceChannel string,
	timeoutHeight clienttypes.Height,
	timeoutTimestamp uint64,
) error {

	sourceChannelEnd, found := k.ChannelKeeper.GetChannel(ctx, sourcePort, sourceChannel)
	if !found {
		return sdkerrors.Wrapf(
			channeltypes.ErrChannelNotFound,
			"port ID (%s) channel ID (%s)",
			sourcePort,
			sourceChannel,
		)
	}

	destinationPort := sourceChannelEnd.GetCounterparty().GetPortID()
	destinationChannel := sourceChannelEnd.GetCounterparty().GetChannelID()

	// get the next sequence
	sequence, found := k.ChannelKeeper.GetNextSequenceSend(ctx, sourcePort, sourceChannel)
	if !found {
		return sdkerrors.Wrapf(
			channeltypes.ErrSequenceSendNotFound,
			"source port: %s, source channel: %s", sourcePort, sourceChannel,
		)
	}

	channelCap, ok := k.ScopedKeeper.GetCapability(
		ctx,
		host.ChannelCapabilityPath(sourcePort, sourceChannel),
	)
	if !ok {
		return sdkerrors.Wrap(
			channeltypes.ErrChannelCapabilityNotFound,
			"module does not own channel capability",
		)
	}

	packetBytes, err := packetData.GetBytes()
	if err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrJSONMarshal, "cannot marshal the packet: "+err.Error())
	}

	packet := channeltypes.NewPacket(
		packetBytes,
		sequence,
		sourcePort,
		sourceChannel,
		destinationPort,
		destinationChannel,
		timeoutHeight,
		timeoutTimestamp,
	)

	if err := k.ChannelKeeper.SendPacket(ctx, channelCap, packet); err != nil {
		return err
	}

	return nil
}

// OnRecvSellOrderPacket processes packet reception
func (k Keeper) OnRecvSellOrderPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	data types.SellOrderPacketData,
) (packetAck types.SellOrderPacketAck, err error) {
	// validate packet data upon receiving
	if err := data.ValidateBasic(); err != nil {
		return packetAck, err
	}

	pairIndex := types.OrderBookIndex(
		packet.SourcePort,
		packet.SourceChannel,
		data.AmountDenom,
		data.PriceDenom,
	)
	book, found := k.GetBuyOrderBook(ctx, pairIndex)
	if !found {
		return packetAck, errors.New("The pair does not exist")
	}

	// fill the sell order
	remaining, liquidated, gain, _ := book.FillSellOrder(types.Order{
		Amount: data.Amount,
		Price:  data.Price,
	})

	// return the remaining amount and gains
	packetAck.RemainingAmount = remaining
	packetAck.Gain = gain

	// before distributing sales, we resolve the denom
	// first check if the denom received comes from this chain originally
	finalAmountDenom, saved := k.OriginalDenom(
		ctx,
		packet.DestinationPort,
		packet.DestinationChannel,
		data.AmountDenom,
	)
	if !saved {
		// if it was not from this chain, we use voucher as denom
		finalAmountDenom = VoucherDenom(packet.SourcePort, packet.SourceChannel, data.AmountDenom)
	}

	// dispatch liquidated buy orders
	for _, liquidation := range liquidated {
		liquidation := liquidation
		addr, err := sdk.AccAddressFromBech32(liquidation.Creator)
		if err != nil {
			return packetAck, err
		}

		err = k.SafeMint(
			ctx,
			packet.DestinationPort,
			packet.DestinationChannel,
			addr,
			finalAmountDenom,
			liquidation.Amount,
		)
		if err != nil {
			return packetAck, err
		}
	}

	// save the new order book
	k.SetBuyOrderBook(ctx, book)

	return packetAck, nil
}

// OnAcknowledgementSellOrderPacket responds to the the success or failure of a packet
// acknowledgement written on the receiving chain.
func (k Keeper) OnAcknowledgementSellOrderPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	data types.SellOrderPacketData,
	ack channeltypes.Acknowledgement,
) error {
	switch dispatchedAck := ack.Response.(type) {
	case *channeltypes.Acknowledgement_Error:
		// in case of error, we mint back the native token
		receiver, err := sdk.AccAddressFromBech32(data.Seller)
		if err != nil {
			return err
		}

		err = k.SafeMint(
			ctx,
			packet.SourcePort,
			packet.SourceChannel,
			receiver,
			data.AmountDenom,
			data.Amount,
		)
		if err != nil {
			return err
		}

		return nil
	case *channeltypes.Acknowledgement_Result:
		// Decode the packet acknowledgment
		var packetAck types.SellOrderPacketAck

		if err := types.ModuleCdc.UnmarshalJSON(dispatchedAck.Result, &packetAck); err != nil {
			// The counter-party module doesn't implement the correct acknowledgment format
			return errors.New("cannot unmarshal acknowledgment")
		}

		// get the sell order book
		pairIndex := types.OrderBookIndex(
			packet.SourcePort,
			packet.SourceChannel,
			data.AmountDenom,
			data.PriceDenom,
		)
		book, found := k.GetSellOrderBook(ctx, pairIndex)
		if !found {
			panic("Sell order book must exist")
		}

		// append the remaining amount of the order
		if packetAck.RemainingAmount > 0 {
			_, err := book.AppendOrder(data.Seller, packetAck.RemainingAmount, data.Price)
			if err != nil {
				return err
			}

			// save the new order book
			k.SetSellOrderBook(ctx, book)
		}

		// mint the gains
		if packetAck.Gain > 0 {
			receiver, err := sdk.AccAddressFromBech32(data.Seller)
			if err != nil {
				return err
			}

			finalPriceDenom, saved := k.OriginalDenom(
				ctx,
				packet.SourcePort,
				packet.SourceChannel,
				data.PriceDenom,
			)
			if !saved {
				// if it was not from this chain, then we use voucher as denom
				finalPriceDenom = VoucherDenom(
					packet.DestinationPort,
					packet.DestinationChannel,
					data.PriceDenom,
				)
			}

			err = k.SafeMint(
				ctx,
				packet.SourcePort,
				packet.SourceChannel,
				receiver,
				finalPriceDenom,
				packetAck.Gain,
			)
			if err != nil {
				return err
			}
		}

		return nil
	default:
		// The counter-party module doesn't implement the correct acknowledgment format
		return errors.New("invalid acknowledgment format")
	}
}

// OnTimeoutSellOrderPacket responds to the case where a packet has not been transmitted because of
// a timeout
func (k Keeper) OnTimeoutSellOrderPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	data types.SellOrderPacketData,
) error {
	// in case of error we mint back the native token
	receiver, err := sdk.AccAddressFromBech32(data.Seller)
	if err != nil {
		return err
	}

	err = k.SafeMint(
		ctx,
		packet.SourcePort,
		packet.SourceChannel,
		receiver,
		data.AmountDenom,
		data.Amount,
	)
	if err != nil {
		return err
	}

	return nil

	return nil
}

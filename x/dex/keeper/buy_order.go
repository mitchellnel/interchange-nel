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

// TransmitBuyOrderPacket transmits the packet over IBC with the specified source port and source
// channel
func (k Keeper) TransmitBuyOrderPacket(
	ctx sdk.Context,
	packetData types.BuyOrderPacketData,
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

// OnRecvBuyOrderPacket processes packet reception
func (k Keeper) OnRecvBuyOrderPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	data types.BuyOrderPacketData,
) (packetAck types.BuyOrderPacketAck, err error) {
	// validate packet data upon receiving
	if err := data.ValidateBasic(); err != nil {
		return packetAck, err
	}

	// check if the sell order book exists
	pairIndex := types.OrderBookIndex(
		packet.SourcePort,
		packet.SourceChannel,
		data.AmountDenom,
		data.PriceDenom,
	)
	book, found := k.GetSellOrderBook(ctx, pairIndex)
	if !found {
		return packetAck, errors.New("The pair doesn't exist")
	}

	// fill buy order
	remaining, liquidated, purchase, _ := book.FillBuyOrder(types.Order{
		Amount: data.Amount,
		Price:  data.Price,
	})

	// return remaining amount and gains
	packetAck.RemainingAmount = remaining.Amount
	packetAck.Purchase = purchase

	// before distributing gains, we resolve the denom
	// first we check if the denom received comes from this chain originally
	finalPriceDenom, saved := k.OriginalDenom(
		ctx,
		packet.DestinationPort,
		packet.DestinationChannel,
		data.PriceDenom,
	)
	if !saved {
		// if it was not from this chain we use voucher as denom
		finalPriceDenom = VoucherDenom(packet.SourcePort, packet.SourceChannel, data.PriceDenom)
	}

	// dispatch liquidated buy order
	for _, liquidation := range liquidated {
		liquidation := liquidation
		addr, err := sdk.AccAddressFromBech32(liquidation.Creator)
		if err != nil {
			return packetAck, err
		}

		if err := k.SafeMint(
			ctx,
			packet.DestinationPort,
			packet.DestinationChannel,
			addr,
			finalPriceDenom,
			liquidation.Amount*liquidation.Price,
		); err != nil {
			return packetAck, err
		}
	}

	// save the new order book
	k.SetSellOrderBook(ctx, book)

	return packetAck, nil
}

// OnAcknowledgementBuyOrderPacket responds to the the success or failure of a packet
// acknowledgement written on the receiving chain.
func (k Keeper) OnAcknowledgementBuyOrderPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	data types.BuyOrderPacketData,
	ack channeltypes.Acknowledgement,
) error {
	switch dispatchedAck := ack.Response.(type) {
	case *channeltypes.Acknowledgement_Error:
		// in case of error we mint back the native token
		receiver, err := sdk.AccAddressFromBech32(data.Buyer)
		if err != nil {
			return err
		}

		if err := k.SafeMint(
			ctx,
			packet.SourcePort,
			packet.SourceChannel,
			receiver,
			data.PriceDenom,
			data.Amount*data.Price,
		); err != nil {
			return err
		}

		return nil
	case *channeltypes.Acknowledgement_Result:
		// decode the packet acknowledgment
		var packetAck types.BuyOrderPacketAck

		if err := types.ModuleCdc.UnmarshalJSON(dispatchedAck.Result, &packetAck); err != nil {
			// The counter-party module doesn't implement the correct acknowledgment format
			return errors.New("cannot unmarshal acknowledgment")
		}

		// get the sell order book
		pairIndex := types.OrderBookIndex(packet.SourcePort, packet.SourceChannel, data.AmountDenom, data.PriceDenom)
		book, found := k.GetBuyOrderBook(ctx, pairIndex)
		if !found {
			panic("buy order book must exist")
		}

		// append the remaining amount of the order
		if packetAck.RemainingAmount > 0 {
			_, err := book.AppendOrder(
				data.Buyer,
				packetAck.RemainingAmount,
				data.Price,
			)
			if err != nil {
				return err
			}

			// save the new order book
			k.SetBuyOrderBook(ctx, book)
		}

		// mint the purchase
		if packetAck.Purchase > 0 {
			receiver, err := sdk.AccAddressFromBech32(data.Buyer)
			if err != nil {
				return err
			}

			finalAmountDenom, saved := k.OriginalDenom(ctx, packet.SourcePort, packet.SourceChannel, data.AmountDenom)
			if !saved {
				// if it was not from this chain we use voucher as denom
				finalAmountDenom = VoucherDenom(packet.DestinationPort, packet.DestinationChannel, data.AmountDenom)
			}

			if err := k.SafeMint(
				ctx,
				packet.SourcePort,
				packet.SourceChannel,
				receiver,
				finalAmountDenom,
				packetAck.Purchase,
			); err != nil {
				return err
			}
		}

		return nil
	default:
		// The counter-party module doesn't implement the correct acknowledgment format
		return errors.New("invalid acknowledgment format")
	}
}

// OnTimeoutBuyOrderPacket responds to the case where a packet has not been transmitted because of a
// timeout
func (k Keeper) OnTimeoutBuyOrderPacket(
	ctx sdk.Context,
	packet channeltypes.Packet,
	data types.BuyOrderPacketData,
) error {
	// in case of error we mint back the native token
	receiver, err := sdk.AccAddressFromBech32(data.Buyer)
	if err != nil {
		return err
	}

	if err := k.SafeMint(
		ctx,
		packet.SourcePort,
		packet.SourceChannel,
		receiver,
		data.PriceDenom,
		data.Amount*data.Price,
	); err != nil {
		return err
	}

	return nil
}

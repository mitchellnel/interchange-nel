package keeper

import (
	"context"
	"errors"

	"interchange-nel/x/dex/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
)

func (k msgServer) SendSellOrder(
	goCtx context.Context,
	msg *types.MsgSendSellOrder,
) (*types.MsgSendSellOrderResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// if an order book doesn't exist, throw an error
	pairIndex := types.OrderBookIndex(msg.Port, msg.ChannelID, msg.AmountDenom, msg.PriceDenom)
	_, found := k.GetSellOrderBook(ctx, pairIndex)
	if !found {
		return &types.MsgSendSellOrderResponse{}, errors.New("The pair doesn't exist")
	}

	// get sender's address
	sender, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return &types.MsgSendSellOrderResponse{}, err
	}

	// use SafeBurn to ensure that no new native tokens are minted
	if err := k.SafeBurn(
		ctx, msg.Port, msg.ChannelID, sender, msg.AmountDenom, msg.Amount,
	); err != nil {
		return &types.MsgSendSellOrderResponse{}, err
	}

	// save the voucher received on the other chain to have the ability to resolve it into the
	// original denom
	k.SaveVoucherDenom(ctx, msg.Port, msg.ChannelID, msg.AmountDenom)

	// Construct the packet
	var packet types.SellOrderPacketData

	packet.Seller = msg.Creator
	packet.AmountDenom = msg.AmountDenom
	packet.Amount = msg.Amount
	packet.PriceDenom = msg.PriceDenom
	packet.Price = msg.Price

	// Transmit the packet
	err = k.TransmitSellOrderPacket(
		ctx,
		packet,
		msg.Port,
		msg.ChannelID,
		clienttypes.ZeroHeight(),
		msg.TimeoutTimestamp,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgSendSellOrderResponse{}, nil
}

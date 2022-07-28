package keeper

import (
	"context"
	"errors"

	"interchange-nel/x/dex/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CancelBuyOrder(
	goCtx context.Context,
	msg *types.MsgCancelBuyOrder,
) (*types.MsgCancelBuyOrderResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// retrieve the book
	pairIndex := types.OrderBookIndex(msg.Port, msg.Channel, msg.AmountDenom, msg.PriceDenom)
	b, found := k.GetBuyOrderBook(ctx, pairIndex)
	if !found {
		return &types.MsgCancelBuyOrderResponse{}, errors.New("The pair doesn't exist")
	}

	// check order creator
	order, err := b.Book.GetOrderFromID(msg.OrderID)
	if err != nil {
		return &types.MsgCancelBuyOrderResponse{}, err
	}

	if order.Creator != msg.Creator {
		return &types.MsgCancelBuyOrderResponse{}, errors.New("Canceller must be creator")
	}

	// remove order
	if err := b.Book.RemoveOrderFromID(msg.OrderID); err != nil {
		return &types.MsgCancelBuyOrderResponse{}, err
	}

	k.SetBuyOrderBook(ctx, b)

	// refund buyer with remaining price amount
	buyer, err := sdk.AccAddressFromBech32(order.Creator)
	if err != nil {
		return &types.MsgCancelBuyOrderResponse{}, err
	}

	if err := k.SafeMint(
		ctx,
		msg.Port,
		msg.Channel,
		buyer,
		msg.PriceDenom,
		order.Amount*order.Price,
	); err != nil {
		return &types.MsgCancelBuyOrderResponse{}, err
	}

	return &types.MsgCancelBuyOrderResponse{}, nil
}

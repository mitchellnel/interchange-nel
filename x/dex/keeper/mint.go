package keeper

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"

	"interchange-nel/x/dex/types"
)

// isIBCToken checks if the token came from the IBC module -- it is a voucher
// Each IBC token start with an ibc/ denom, the check is rather simple
func isIBCToken(denom string) bool {
	return strings.HasPrefix(denom, "ibc/")
}

func (k Keeper) SafeBurn(
	ctx sdk.Context,
	port string,
	channel string,
	sender sdk.AccAddress,
	denom string,
	amount int32,
) error {
	if isIBCToken(denom) {
		// burn the tokens (vouchers)
		if err := k.BurnTokens(
			ctx, sender, sdk.NewCoin(denom, sdk.NewInt(int64(amount))),
		); err != nil {
			return err
		}
	} else {
		// lock the tokens
		if err := k.LockTokens(
			ctx, port, channel, sender, sdk.NewCoin(denom, sdk.NewInt(int64(amount))),
		); err != nil {
			return err
		}
	}

	return nil
}

func (k Keeper) BurnTokens(ctx sdk.Context, sender sdk.AccAddress, tokens sdk.Coin) error {
	// transfer the coins to the bank module and burn them
	if err := k.bankKeeper.SendCoinsFromAccountToModule(
		ctx, sender, types.ModuleName, sdk.NewCoins(tokens),
	); err != nil {
		return err
	}

	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(tokens)); err != nil {
		// NOTE: this should not happen as the module accounts was retrieved on the step above, and
		// it has enough balance to burn
		panic(fmt.Sprintf("Cannot burn coins after a successful send to a module account: %v", err))
	}

	return nil
}

func (k Keeper) LockTokens(
	ctx sdk.Context,
	sourcePort string,
	sourceChannel string,
	sender sdk.AccAddress,
	tokens sdk.Coin,
) error {
	// create the escrow address for the tokens
	escrowAddress := ibctransfertypes.GetEscrowAddress(sourcePort, sourceChannel)

	// escrow source tokens
	// it fails is balance is insufficient
	if err := k.bankKeeper.SendCoins(
		ctx, sender, escrowAddress, sdk.NewCoins(tokens),
	); err != nil {
		return err
	}

	return nil
}

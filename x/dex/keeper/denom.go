package keeper

import (
	"interchange-nel/x/dex/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
)

func (k Keeper) SaveVoucherDenom(ctx sdk.Context, port string, channel string, denom string) {
	voucher := VoucherDenom(port, channel, denom)

	// store the origin denom
	_, saved := k.GetDenomTrace(ctx, voucher)
	if !saved {
		k.SetDenomTrace(ctx, types.DenomTrace{
			Index:   voucher,
			Port:    port,
			Channel: channel,
			Origin:  denom,
		})
	}
}

func VoucherDenom(port string, channel string, denom string) string {
	// since sendPacket did not prefix the denomination, we must prefix it here
	sourcePrefix := ibctransfertypes.GetDenomPrefix(port, channel)

	// sourcePrefix already contains the trailing "/"
	prefixedDenom := sourcePrefix + denom

	// construct the denomTrace from the full raw denomination
	denomTrace := ibctransfertypes.ParseDenomTrace(prefixedDenom)
	voucher := denomTrace.IBCDenom()

	return voucher[:16]
}

func (k Keeper) OriginalDenom(
	ctx sdk.Context,
	port string,
	channel string,
	voucher string,
) (string, bool) {
	trace, exist := k.GetDenomTrace(ctx, voucher)
	if exist {
		// check if original port and channel
		if trace.Port == port && trace.Channel == channel {
			return trace.Origin, true
		}
	}

	// not the original chain
	return "", false
}

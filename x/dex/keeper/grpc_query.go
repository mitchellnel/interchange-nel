package keeper

import (
	"interchange-nel/x/dex/types"
)

var _ types.QueryServer = Keeper{}

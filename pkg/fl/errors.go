package fl

import "errors"

var ErrNoUpdates = errors.New("no updates provided for aggregation")
var ErrOverflow = errors.New("sample count overflow during aggregation")

package wago

import (
	"os"
)

func preparedIntCallBlockSetting() bool {
	if value, ok := os.LookupEnv("WAGO_PREPARED_INT_CALL_BLOCK"); ok {
		return value != "0"
	}
	return preparedIntCallBlockDefault
}

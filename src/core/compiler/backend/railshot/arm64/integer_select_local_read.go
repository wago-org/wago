//go:build arm64

package arm64

import "os"

var integerSelectLocalReadEnabled = os.Getenv("WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ") != "1"

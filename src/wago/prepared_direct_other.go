//go:build (!amd64 && !arm64) || tinygo || (arm64 && !(linux || darwin || windows))

package wago

import "fmt"

const preparedDirectIntSupported = false
const preparedDirectIntPrivateSupported = false

func (fn *PreparedFunction) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

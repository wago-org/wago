//go:build arm64 && tinygo && (linux || darwin)

package wago

import "fmt"

const invokeCachedPreboundIntSupported = false

type cachedInvokeIntCall struct{}

func (in *Instance) prepareCachedInvokeIntCall(*cachedInvokeIntCall, uintptr, uintptr) {}

func (in *Instance) invokeCachedPreboundInt(*invokeCache, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: cached prebound integer entry is unavailable")
}

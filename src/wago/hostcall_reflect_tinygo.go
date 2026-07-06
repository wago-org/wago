//go:build tinygo

package wago

import "fmt"

// reflectSyncHost is unavailable under TinyGo: its reflect package cannot call
// arbitrary functions (reflect.Value.Call is unimplemented). TinyGo builds must
// provide host imports as wago.HostFunc (the reflection-free slot form).
func reflectSyncHost(v any, _ FuncSig) (HostFunc, error) {
	return nil, fmt.Errorf("wago: native-function host imports need standard Go; use wago.HostFunc under TinyGo (got %T)", v)
}

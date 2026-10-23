package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func valTypeEqual(a, b wasm.ValType) bool { return wasm.EqualValType(a, b) }

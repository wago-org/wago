//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func installRV64StorageFreeProof(*Compiled, *wasm.Module) {}

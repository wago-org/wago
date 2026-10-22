//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCStructNumericGlobalsWire(data []byte) bool { return false }

func AdmitGCStructNumericGlobalsSource(data []byte) bool { return false }

func admitRV64GCStructNumericGlobals(m *wasm.Module) bool { return false }

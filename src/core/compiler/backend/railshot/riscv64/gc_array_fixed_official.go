//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayFixedOfficialWire(data []byte) bool { return false }

func AdmitGCArrayFixedOfficialSource(data []byte) bool { return false }

func admitRV64GCArrayFixedOfficial(m *wasm.Module) bool { return false }

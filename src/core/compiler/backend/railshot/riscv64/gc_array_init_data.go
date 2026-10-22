//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayInitDataWire(data []byte) bool { return false }

func AdmitGCArrayInitDataSource(data []byte) bool { return false }

func admitRV64GCArrayInitData(m *wasm.Module) bool { return false }

func admitRV64GCArrayInitDataWide(m *wasm.Module) bool { return false }

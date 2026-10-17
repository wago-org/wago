//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingNonFlatExportWire(data []byte) bool { return false }

func GCTypeSubtypingNonFlatExportSource(data []byte) bool { return false }

func admitRV64GCTypeSubtypingNonFlatExport(m *wasm.Module) bool { return false }

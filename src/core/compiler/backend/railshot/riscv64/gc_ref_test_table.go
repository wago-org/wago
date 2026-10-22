//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCRefTestTableWire(data []byte) bool { return false }

// AdmitGCRefTestTableSource is the exact-wire witness for the bounded rooted
// object-table allocation and ref.test checkpoint.
func AdmitGCRefTestTableSource(data []byte) bool { return false }

func admitRV64GCRefTestTable(m *wasm.Module) bool { return false }

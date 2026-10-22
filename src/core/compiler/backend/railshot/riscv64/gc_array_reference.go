//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayReferenceWire(data []byte) bool { return false }

func AdmitGCArrayReferenceSource(data []byte) bool { return false }

func admitRV64GCArrayReference(m *wasm.Module) bool { return false }

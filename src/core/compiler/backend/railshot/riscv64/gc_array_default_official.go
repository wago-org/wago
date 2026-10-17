//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayDefaultOfficialWire(data []byte) bool { return false }

func AdmitGCArrayDefaultOfficialSource(data []byte) bool { return false }

func admitRV64GCArrayOfficial(m *wasm.Module) bool { return false }

func admitRV64GCArrayDefaultOfficial(m *wasm.Module) bool { return false }

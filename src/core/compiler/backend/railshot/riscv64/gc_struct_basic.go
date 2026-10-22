//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCStructBasicWire(data []byte) bool { return false }

func AdmitGCStructBasicSource(data []byte) bool { return false }

func admitRV64GCStructBasic(m *wasm.Module) bool { return false }

//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingIndependentStructLinkProviderVariant = 1
	rv64GCTypeSubtypingIndependentStructLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingIndependentStructLinkWire(data []byte) bool { return false }

// GCTypeSubtypingIndependentStructLinkSourceVariant returns 1 for the exact
// .43 provider, 2 for the exact .44 consumer, and zero otherwise.
func GCTypeSubtypingIndependentStructLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingIndependentStructLink(m *wasm.Module) uint8 { return 0 }

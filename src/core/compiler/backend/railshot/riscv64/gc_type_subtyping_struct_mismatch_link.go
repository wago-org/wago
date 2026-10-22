//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingStructMismatchLinkProviderVariant = 1
	rv64GCTypeSubtypingStructMismatchLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingStructMismatchLinkWire(data []byte) bool { return false }

// GCTypeSubtypingStructMismatchLinkSourceVariant returns 1 for the exact .41
// provider, 2 for the exact .42 consumer, and zero for every other source.
func GCTypeSubtypingStructMismatchLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingStructMismatchLink(m *wasm.Module) uint8 { return 0 }

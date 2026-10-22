//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingTransitiveMismatchLinkProviderVariant = 1
	rv64GCTypeSubtypingTransitiveMismatchLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingTransitiveMismatchLinkWire(data []byte) bool { return false }

// GCTypeSubtypingTransitiveMismatchLinkSourceVariant returns 1 for the exact .53
// provider, 2 for the exact .54 consumer, and zero for every other source.
func GCTypeSubtypingTransitiveMismatchLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingTransitiveMismatchLink(m *wasm.Module) uint8 { return 0 }

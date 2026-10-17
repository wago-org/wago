//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingStructProjectionLinkProviderVariant = 1
	rv64GCTypeSubtypingStructProjectionLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingStructProjectionLinkWire(data []byte) bool { return false }

// GCTypeSubtypingStructProjectionLinkSourceVariant returns 1 for the exact
// .39 provider, 2 for the exact .40 consumer, and zero for every other source.
func GCTypeSubtypingStructProjectionLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingStructProjectionLink(m *wasm.Module) uint8 { return 0 }

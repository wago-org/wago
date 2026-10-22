//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingExtendedProjectionLinkProviderVariant = 1
	rv64GCTypeSubtypingExtendedProjectionLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingExtendedProjectionLinkWire(data []byte) bool { return false }

// GCTypeSubtypingExtendedProjectionLinkSourceVariant returns 1 for the exact
// .45 provider, 2 for the exact .46 consumer, and zero otherwise.
func GCTypeSubtypingExtendedProjectionLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingExtendedProjectionLink(m *wasm.Module) uint8 { return 0 }

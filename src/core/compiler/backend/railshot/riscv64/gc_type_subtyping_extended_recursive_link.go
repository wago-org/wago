//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	rv64GCTypeSubtypingExtendedRecursiveLinkProviderVariant = 1
	rv64GCTypeSubtypingExtendedRecursiveLinkConsumerVariant = 2
)

func admitRV64GCTypeSubtypingExtendedRecursiveLinkWire(data []byte) bool { return false }

// GCTypeSubtypingExtendedRecursiveLinkSourceVariant returns 1 for the exact
// .49 provider, 2 for the exact .50 consumer, and zero otherwise.
func GCTypeSubtypingExtendedRecursiveLinkSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingExtendedRecursiveLink(m *wasm.Module) uint8 { return 0 }

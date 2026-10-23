//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/frontend"
)

func railshotStoreTableCapacities(*Compiled, []frontend.TableRuntimeShape) error { return nil }

func railshotTableCapacitySnapshotBytes(*Compiled) uintptr { return 0 }

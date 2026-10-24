//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/frontend"

func (*Compiled) rv64ExecutionTableCapacities() []uint32                         { return nil }
func (*Compiled) setRV64ExecutionTableCapacities([]uint32)                       {}
func railshotStoreTableCapacities(*Compiled, []frontend.TableRuntimeShape) error { return nil }
func railshotValidateTableCapacities(*Compiled) error                            { return nil }
func railshotTableReservedCapacity(*Compiled, int) (int, bool)                   { return 0, false }
func railshotTableCapacitySnapshotBytes(*Compiled) uintptr                       { return 0 }

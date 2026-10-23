//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func (*Compiled) rv64ExecutionTableCapacities() []uint32   { return nil }
func (*Compiled) setRV64ExecutionTableCapacities([]uint32) {}

func railshotValidateTableCapacities(*Compiled) error          { return nil }
func railshotTableReservedCapacity(*Compiled, int) (int, bool) { return 0, false }

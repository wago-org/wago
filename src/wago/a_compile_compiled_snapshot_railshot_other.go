//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func railshotInitializeExecutionSnapshot(*Compiled) error { return nil }

func railshotExecutionSnapshotCacheBytes(*Compiled) uintptr { return 0 }

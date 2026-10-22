//go:build !linux || !riscv64 || wago_precompiled || tinygo || wago_target_tinygo

package wago

func railshotConfigureResourceLimits(*RuntimeConfig, *railshotCompileOptions) {}

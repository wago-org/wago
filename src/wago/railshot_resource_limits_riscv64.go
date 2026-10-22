//go:build linux && riscv64 && !wago_precompiled && !tinygo && !wago_target_tinygo

package wago

func railshotConfigureResourceLimits(cfg *RuntimeConfig, opts *railshotCompileOptions) {
	opts.MaxFunctionLocals = cfg.maxFunctionLocals
	opts.MaxModuleBytes = cfg.maxModuleBytes
	opts.MaxNativeCodeBytes = cfg.maxNativeCodeBytes
	opts.MaxCompiledMetadataBytes = normalizeSnapshotLimit(cfg.maxCompiledMetadataBytes)
}

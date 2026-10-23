//go:build !riscv64

package wago

func railshotRecordMappedCodeBytes(*Compiled, int) {}

func railshotStagedTailFeatures(*RuntimeConfig) CoreFeatures { return 0 }

func railshotStagedGCFeatures(*RuntimeConfig) CoreFeatures { return 0 }

func railshotConfigureNativeTail(CoreFeatures, *RuntimeConfig, *railshotCompileOptions) {}

func railshotStoreNativeTail(*Compiled, *railshotCompiledModule, *RuntimeConfig) error { return nil }

func railshotRetryNativeTail(*Compiled) error { return nil }

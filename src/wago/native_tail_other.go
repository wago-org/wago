//go:build !riscv64

package wago

type railshotRuntimeConfigState struct{}
type railshotTailCompiledState struct{}

func railshotRecordMappedCodeBytes(*Compiled, int) {}

func railshotStagedTailFeatures(*RuntimeConfig) CoreFeatures                           { return 0 }
func railshotConfigureNativeTail(CoreFeatures, *railshotCompileOptions)                {}
func railshotStoreNativeTail(*Compiled, *railshotCompiledModule, *RuntimeConfig) error { return nil }
func railshotWriteNativeTail(*compiledWriter, *Compiled) error                         { return nil }
func railshotReadNativeTail(*compiledReader, *Compiled) error                          { return nil }
func railshotPrepareNativeTail(*Instance, resolvedImports, uintptr, []int, map[uint32]uint64) error {
	return nil
}
func railshotReleaseNativeTail(*Instance)        {}
func railshotRetryNativeTail(*Compiled) error    { return nil }
func railshotNativeTailArenaBytes(*Compiled) int { return 0 }
func railshotNativeCodeBytes(c *Compiled) uint64 { return uint64(len(c.code)) }

func railshotNativeTailImport(*Compiled, int) bool { return false }

type railshotInstantiateTailPolicy struct{}

func railshotInstanceTailPolicy(*RuntimeConfig) railshotInstantiateTailPolicy {
	return railshotInstantiateTailPolicy{}
}
func railshotCaptureNativeTailPolicy(*Instance, InstantiateOptions, int) {}

//go:build !riscv64

package wago

import "fmt"

type functionDependencyAttachments struct{}
type memoryDependencyAttachments struct{}

func railshotAcquireMemoryDependency(_ *memoryImportAttachments, memory *Memory, _ *resourceCleanup) error {
	return memory.attachImporter()
}
func railshotDrainBuilderMemoryDependencies(*memoryImportAttachments) bool  { return false }
func railshotHasBuilderMemoryDependencies(*memoryImportAttachments) bool    { return false }
func railshotInstallMemoryDependencies(*Instance, *memoryImportAttachments) {}
func railshotAdoptMemoryDependencies(*Instance, bool) error                 { return nil }
func railshotHasInstanceMemoryDependencies(*Instance) bool                  { return false }
func railshotDrainInstanceMemoryDependencies(*Instance) bool                { return false }
func railshotTransferMemoryDependency(*Instance, *Memory) bool              { return false }
func railshotTransferredMemoryDependency(*Instance, *Memory) (bool, bool)   { return false, false }

func railshotAcquireFunctionDependency(_ *functionImportAttachments, producer *Instance) error {
	if !producer.retainResourceRoot() {
		return fmt.Errorf("producer instance is closed")
	}
	return nil
}
func railshotDrainBuilderFunctionDependencies(*functionImportAttachments) bool  { return false }
func railshotDrainInstanceFunctionDependencies(*Instance) bool                  { return false }
func railshotHasBuilderFunctionDependencies(*functionImportAttachments) bool    { return false }
func railshotInstallFunctionDependencies(*Instance, *functionImportAttachments) {}
func railshotAdoptFunctionDependencies(*Instance, bool) error                   { return nil }
func railshotFunctionDependencyCleanupCompleted(*Instance)                      {}

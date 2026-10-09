//go:build !riscv64

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

type functionDependencyAttachments struct{}
type memoryDependencyAttachments struct{}
type globalDependencyAttachments struct{}

func railshotAcquireGlobalDependency(_ *globalImportAttachments, global *Global, store *referenceStore, collector *gc.Collector, _ *resourceCleanup) error {
	if isReferenceValType(global.Type) {
		return global.attachReferenceImporterWithCollector(store, collector)
	}
	return global.attachNumericImporter()
}
func railshotDrainBuilderGlobalDependencies(*globalImportAttachments) bool  { return false }
func railshotHasBuilderGlobalDependencies(*globalImportAttachments) bool    { return false }
func railshotInstallGlobalDependencies(*Instance, *globalImportAttachments) {}
func railshotAdoptGlobalDependencies(*Instance, bool) error                 { return nil }
func railshotDrainInstanceGlobalDependencies(*Instance) bool                { return false }
func railshotTransferGlobalDependency(*Instance, *Global) bool              { return false }
func railshotTransferredGlobalDependency(*Instance, *Global) (bool, bool)   { return false, false }

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

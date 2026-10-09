//go:build !riscv64

package wago

import "fmt"

type functionDependencyAttachments struct{}

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

//go:build !riscv64

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

func railshotNativeContextTrapABI() bool                               { return false }
func railshotNativeImportArenaBytes(*Compiled, resolvedImports) int    { return 0 }
func railshotNativeContextIdentity(*coreruntime.JobMemory) uintptr     { return 0 }
func railshotSetNativeContextIdentity(*coreruntime.JobMemory, uintptr) {}
func railshotAlignNativeImportBlob(blob []byte) []byte                 { return blob }
func railshotNativeImportCode(*Compiled, *InstanceExport, *coreruntime.Arena, uintptr, uintptr, int) ([]byte, bool, error) {
	return nil, false, nil
}
func railshotTrapOwner(root *Instance, _ *coreruntime.TrapError) (*Instance, bool) { return root, true }

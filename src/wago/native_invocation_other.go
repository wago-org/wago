//go:build !riscv64

package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

const railshotNativeInvocationContextBytes = coreruntime.InstanceContextBytes

func railshotBindNativeInvocation(*Instance)                        {}
func railshotRestoreNativeInvocation(*Instance, *Instance, uintptr) {}
func railshotSaveNativeInvocation(*Instance) uintptr                { return 0 }
func railshotRestoreSavedNativeInvocation(*Instance, uintptr)       {}

func railshotParkedNativeInvocation(*Instance) uintptr { return 0 }

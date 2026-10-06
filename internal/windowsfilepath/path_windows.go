//go:build windows

// Package windowsfilepath adapts Go paths for direct Win32 calls.
package windowsfilepath

import (
	"os"

	"golang.org/x/sys/windows"
)

const extendedPathThreshold = 248

var canUseLongPaths = func() bool {
	version := windows.RtlGetVersion()
	return version.MajorVersion > 10 || version.MajorVersion == 10 &&
		(version.MinorVersion > 0 || version.MinorVersion == 0 && version.BuildNumber >= 15063)
}()

// UTF16PtrFromString returns a Windows path pointer with the extended-length
// prefix when the ordinary Win32 path limit could apply. The os package does
// this conversion internally; callers that use x/sys/windows directly must do
// it themselves or regress paths that os.Open and os.WriteFile accept.
func UTF16PtrFromString(path string) (*uint16, error) {
	return windows.UTF16PtrFromString(Normalize(path))
}

// Normalize matches the long-path adaptation performed by the Go os package.
// Modern Go processes are marked long-path-aware on supported Windows releases,
// where preserving the path text also preserves trailing-dot and device-name
// semantics. Older releases need the extended prefix.
func Normalize(path string) string {
	if canUseLongPaths {
		return path
	}
	return addExtendedPrefix(path)
}

func addExtendedPrefix(path string) string {
	if path == "" {
		return path
	}
	if hasExtendedPrefix(path) || hasDevicePrefix(path) {
		return path
	}
	if len(path) < extendedPathThreshold {
		full, err := windows.FullPath(path)
		if err != nil || len(full) < extendedPathThreshold {
			return path
		}
		path = full
	} else {
		full, err := windows.FullPath(path)
		if err != nil {
			// Preserve the original error behavior. The eventual Win32 call can
			// report the path-specific failure more accurately than this adapter.
			return path
		}
		path = full
	}
	if len(path) >= 2 && os.IsPathSeparator(path[0]) && os.IsPathSeparator(path[1]) {
		return `\\?\UNC\` + path[2:]
	}
	return `\\?\` + path
}

func hasExtendedPrefix(path string) bool {
	return len(path) >= 4 && (path[:4] == `\??\` ||
		os.IsPathSeparator(path[0]) && os.IsPathSeparator(path[1]) && path[2] == '?' && os.IsPathSeparator(path[3]))
}

func hasDevicePrefix(path string) bool {
	return len(path) >= 4 && os.IsPathSeparator(path[0]) && os.IsPathSeparator(path[1]) &&
		path[2] == '.' && os.IsPathSeparator(path[3])
}

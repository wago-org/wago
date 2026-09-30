//go:build linux && !tinygo

package artifactcache

import "syscall"

const cacheDirectoryOpenFlags = syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_DIRECTORY | syscall.O_NOFOLLOW

func openCacheDirectory(path string) (int, error) {
	return syscall.Open(path, cacheDirectoryOpenFlags, 0)
}

func openCacheDirectoryAt(directory int, name string) (int, error) {
	return syscall.Openat(directory, name, cacheDirectoryOpenFlags, 0)
}

func duplicateCacheDescriptor(fd int) (int, error) { return syscall.Dup(fd) }

func closeCacheDescriptor(fd int) error { return syscall.Close(fd) }

func unlinkCacheEntryAt(directory int, name string) error { return syscall.Unlinkat(directory, name) }

//go:build linux && tinygo

package artifactcache

import (
	"os"
	"strconv"
	"syscall"
)

const cacheDirectoryOpenFlags = syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_DIRECTORY | syscall.O_NOFOLLOW

func openCacheDirectory(path string) (int, error) {
	return syscall.Open(path, cacheDirectoryOpenFlags, 0)
}

func openCacheDirectoryAt(directory int, name string) (int, error) {
	return syscall.Open(cacheDescriptorPath(directory)+"/"+name, cacheDirectoryOpenFlags, 0)
}

func duplicateCacheDescriptor(fd int) (int, error) {
	return syscall.Open(cacheDescriptorPath(fd), cacheDirectoryOpenFlags&^syscall.O_NOFOLLOW, 0)
}

func closeCacheDescriptor(fd int) error { return syscall.Close(fd) }

func unlinkCacheEntryAt(directory int, name string) error {
	return os.Remove(cacheDescriptorPath(directory) + "/" + name)
}

func cacheDescriptorPath(fd int) string { return "/proc/self/fd/" + strconv.Itoa(fd) }

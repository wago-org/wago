//go:build darwin

package artifactcache

import "golang.org/x/sys/unix"

const cacheDirectoryOpenFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_DIRECTORY | unix.O_NOFOLLOW

func openCacheDirectory(path string) (int, error) {
	return unix.Open(path, cacheDirectoryOpenFlags, 0)
}

func openCacheDirectoryAt(directory int, name string) (int, error) {
	return unix.Openat(directory, name, cacheDirectoryOpenFlags, 0)
}

func duplicateCacheDescriptor(fd int) (int, error) { return unix.Dup(fd) }

func closeCacheDescriptor(fd int) error { return unix.Close(fd) }

func unlinkCacheEntryAt(directory int, name string) error { return unix.Unlinkat(directory, name, 0) }

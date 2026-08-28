//go:build !linux && !darwin && !windows

package artifactcache

import (
	"errors"
	"os"
)

type cacheRoot struct{}

func openCacheRoot(string) (*cacheRoot, error) {
	return nil, errors.New("secure cache pruning is unsupported on this platform")
}

func (*cacheRoot) close() error { return nil }

func (*cacheRoot) scan(func(string, string, os.FileInfo) error) error { return nil }

func (*cacheRoot) remove(string) error { return nil }

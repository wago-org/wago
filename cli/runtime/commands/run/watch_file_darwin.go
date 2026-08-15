//go:build darwin && !wago_lean

package run

import (
	"fmt"
	"os"
	"syscall"
)

func metadataForWatchedFile(_ *os.File, info os.FileInfo) (watchedFileMetadata, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return watchedFileMetadata{}, fmt.Errorf("read Darwin module metadata")
	}
	return watchedFileMetadata{
		size:     info.Size(),
		modified: info.ModTime().UnixNano(),
		change:   [4]uint64{uint64(stat.Dev), stat.Ino, uint64(stat.Ctimespec.Sec), uint64(stat.Ctimespec.Nsec)},
	}, nil
}

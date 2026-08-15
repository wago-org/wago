//go:build linux && !wago_lean

package run

import (
	"fmt"
	"os"
	"syscall"
)

func metadataForWatchedFile(_ *os.File, info os.FileInfo) (watchedFileMetadata, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return watchedFileMetadata{}, fmt.Errorf("read Linux module metadata")
	}
	return watchedFileMetadata{
		size:     info.Size(),
		modified: info.ModTime().UnixNano(),
		change:   [4]uint64{uint64(stat.Dev), stat.Ino, uint64(stat.Ctim.Sec), uint64(stat.Ctim.Nsec)},
	}, nil
}

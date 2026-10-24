//go:build linux && !tinygo && (amd64 || arm64 || (riscv64 && !wago_target_tinygo))

package runtime

import (
	"errors"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const compilerFileThreshold = 1 << 20

type compilerCodeFile struct {
	file *os.File
	page int
}

// Compiler images are exclusively owned between allocation and publication.
// The per-buffer directory also retains handles for a failed growth retirement.
type compilerImageOwner struct {
	files   map[uintptr]compilerCodeFile
	reserve func(int, int64) error // optional per-owner fault injection
}

func compilerCodeBufferOps() *codeBufferOps {
	owner := &compilerImageOwner{}
	return &codeBufferOps{
		mapRW:      owner.allocate,
		afterUnmap: owner.closeFile,
		take:       owner.closeFile,
		seal: func(mem []byte) error {
			if err := SealCode(mem); err != nil {
				return err
			}
			return owner.closeFile(mem)
		},
		reclaim: owner.reclaim,
		finish:  owner.finish,
		hasFile: func(mem []byte) bool { _, ok := owner.files[compilerImageKey(mem)]; return ok },
	}
}

func compilerImageKey(mem []byte) uintptr {
	if len(mem) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.SliceData(mem)))
}

func (o *compilerImageOwner) allocate(n int) ([]byte, error) {
	if n < compilerFileThreshold || n > int(^uint(0)>>1)/2-os.Getpagesize() {
		return mmapCodeRW(n)
	}
	// O_TMPFILE avoids named-file cleanup and leaves no discoverable pathname.
	fd, err := unix.Open(os.TempDir(), unix.O_TMPFILE|unix.O_RDWR|unix.O_CLOEXEC, 0600)
	if err != nil {
		return mmapCodeRW(n)
	}
	file := os.NewFile(uintptr(fd), "wago-compiler-image")
	fallback := func() ([]byte, error) {
		if err := file.Close(); err != nil {
			return nil, err
		}
		return mmapCodeRW(n)
	}
	var fs unix.Statfs_t
	if unix.Fstatfs(fd, &fs) != nil || fs.Flags&unix.ST_NOEXEC != 0 || fs.Type == unix.TMPFS_MAGIC || fs.Type == unix.RAMFS_MAGIC {
		return fallback()
	}
	page := os.Getpagesize()
	capacity := (n*2 + page - 1) &^ (page - 1)
	// Reserve disk blocks before exposing writable pages: expected disk-space
	// failures must use the anonymous fallback, rather than faulting with SIGBUS.
	reserve := o.reserve
	if reserve == nil {
		reserve = func(fd int, n int64) error { return unix.Fallocate(fd, 0, 0, n) }
	}
	if file.Truncate(int64(capacity)) != nil || reserve(fd, int64(capacity)) != nil {
		return fallback()
	}
	mem, err := syscall.Mmap(fd, 0, capacity, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return fallback()
	}
	if o.files == nil {
		o.files = make(map[uintptr]compilerCodeFile)
	}
	o.files[compilerImageKey(mem)] = compilerCodeFile{file: file, page: page}
	return mem, nil
}

func (o *compilerImageOwner) closeFile(mem []byte) error {
	key := compilerImageKey(mem)
	entry, ok := o.files[key]
	if !ok {
		return nil
	}
	// Close consumes the descriptor even when it reports an error. Never retry
	// it: a later descriptor may already reuse that number.
	delete(o.files, key)
	return entry.file.Close()
}

func (o *compilerImageOwner) reclaim(mem []byte, start, end int) (int, error) {
	entry, ok := o.files[compilerImageKey(mem)]
	if !ok {
		return end, nil
	}
	originalStart := start
	page := entry.page
	start = (start + page - 1) &^ (page - 1)
	end = end &^ (page - 1)
	if start >= end {
		return originalStart, nil
	}
	if err := unix.SyncFileRange(int(entry.file.Fd()), int64(start), int64(end-start), unix.SYNC_FILE_RANGE_WRITE); err != nil {
		return start, err
	}
	if err := madviseDontNeed(mem[start:end]); err != nil {
		return start, err
	}
	if err := unix.Fadvise(int(entry.file.Fd()), 0, int64(end), unix.FADV_DONTNEED); err != nil {
		return start, err
	}
	return end, nil
}

func (o *compilerImageOwner) finish(mem []byte, used int) error {
	entry, ok := o.files[compilerImageKey(mem)]
	if !ok {
		return nil
	}
	if err := unix.Fdatasync(int(entry.file.Fd())); err != nil {
		return err
	}
	end := used &^ (entry.page - 1)
	return errors.Join(madviseDontNeed(mem[:end]), unix.Fadvise(int(entry.file.Fd()), 0, int64(end), unix.FADV_DONTNEED))
}

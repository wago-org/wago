//go:build linux && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	uffdAPI                  = uint64(0xaa)
	uffdUserModeOnly         = uintptr(1)
	uffdFeatureWPUnpopulated = uint64(1 << 13)
	uffdFeatureWPAsync       = uint64(1 << 15)
	uffdRegisterModeWP       = uint64(1 << 1)
	uffdWriteProtectModeWP   = uint64(1)

	pageIsWritten = uint64(1 << 1)

	uffdioAPI          = uintptr(0xc018aa3f)
	uffdioRegister     = uintptr(0xc020aa00)
	uffdioWriteProtect = uintptr(0xc018aa06)
	pagemapScan        = uintptr(0xc0606610)

	dirtyRegionCapacity = 256

	dirtyTrackingFull    = uint32(0)
	dirtyTrackingUFFD    = uint32(1)
	dirtyTrackingPagemap = uint32(2)

	pagemapFile    = uint64(1 << 61)
	pagemapSwapped = uint64(1 << 62)
	pagemapPresent = uint64(1 << 63)
)

type uffdioAPIArg struct {
	API      uint64
	Features uint64
	IOCTLs   uint64
}

type uffdioRegisterArg struct {
	Start  uint64
	Len    uint64
	Mode   uint64
	IOCTLs uint64
}

type uffdioWriteProtectArg struct {
	Start uint64
	Len   uint64
	Mode  uint64
}

type pageRegion struct {
	Start      uint64
	End        uint64
	Categories uint64
}

type pagemapScanArg struct {
	Size              uint64
	Flags             uint64
	Start             uint64
	End               uint64
	WalkEnd           uint64
	Vec               uint64
	VecLen            uint64
	MaxPages          uint64
	CategoryInverted  uint64
	CategoryMask      uint64
	CategoryAnyOfMask uint64
	ReturnMask        uint64
}

type filePageSnapshotTracker struct {
	snapshot    *filePageSnapshot
	addr        uintptr
	size        uintptr
	prefix      uintptr
	uffd        int
	pagemap     int
	regions     [dirtyRegionCapacity]pageRegion
	pagemapBuf  []byte
	mode        atomic.Uint32
	fallbackErr error
}

func (s *filePageSnapshot) track(addr uintptr, size, prefix int) (pageSnapshotDirtyTracker, error) {
	t := &filePageSnapshotTracker{
		snapshot: s,
		addr:     addr,
		size:     uintptr(size),
		prefix:   uintptr(prefix),
		uffd:     -1,
		pagemap:  -1,
	}
	uffdErr := t.enableUFFD()
	if uffdErr == nil {
		t.mode.Store(dirtyTrackingUFFD)
		return t, nil
	}
	t.disable()
	if err := t.enablePagemap(); err != nil {
		t.fallbackErr = errors.Join(uffdErr, err)
		t.disable()
		return t, nil
	}
	t.mode.Store(dirtyTrackingPagemap)
	return t, nil
}

func (t *filePageSnapshotTracker) enableUFFD() error {
	fd, _, errno := syscall.Syscall(
		sysUserfaultfd,
		uintptr(syscall.O_CLOEXEC|syscall.O_NONBLOCK)|uffdUserModeOnly,
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("userfaultfd: %w", errno)
	}
	t.uffd = int(fd)

	api := uffdioAPIArg{
		API:      uffdAPI,
		Features: uffdFeatureWPAsync | uffdFeatureWPUnpopulated,
	}
	if err := ioctl(t.uffd, uffdioAPI, unsafe.Pointer(&api)); err != nil {
		return fmt.Errorf("UFFDIO_API: %w", err)
	}
	if api.Features&uffdFeatureWPAsync == 0 {
		return errors.New("UFFD_FEATURE_WP_ASYNC is unavailable")
	}

	register := uffdioRegisterArg{
		Start: uint64(t.addr),
		Len:   uint64(t.prefix),
		Mode:  uffdRegisterModeWP,
	}
	if err := ioctl(t.uffd, uffdioRegister, unsafe.Pointer(&register)); err != nil {
		return fmt.Errorf("UFFDIO_REGISTER: %w", err)
	}
	if err := t.writeProtect(t.addr, t.prefix); err != nil {
		return err
	}

	pagemap, err := syscall.Open("/proc/self/pagemap", syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open pagemap: %w", err)
	}
	t.pagemap = pagemap
	return nil
}

func (t *filePageSnapshotTracker) enablePagemap() error {
	pagemap, err := syscall.Open("/proc/self/pagemap", syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open pagemap: %w", err)
	}
	t.pagemap = pagemap
	pages := (t.prefix + uintptr(pageSize) - 1) / uintptr(pageSize)
	t.pagemapBuf = make([]byte, int(pages)*8)
	return nil
}

func ioctl(fd int, request uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), request, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

func (t *filePageSnapshotTracker) reset() error {
	t.snapshot.mu.RLock()
	defer t.snapshot.mu.RUnlock()
	if t.snapshot.closed {
		return errors.New("wago: page snapshot is closed")
	}
	switch t.mode.Load() {
	case dirtyTrackingUFFD:
		if err := t.discardWritten(); err != nil {
			t.disable()
			return t.discardAll()
		}
		return t.discardHeap()
	case dirtyTrackingPagemap:
		if err := t.discardPrivateCOW(); err != nil {
			t.disable()
			return t.discardAll()
		}
		return t.discardHeap()
	default:
		return t.discardAll()
	}
}

func (t *filePageSnapshotTracker) discardWritten() error {
	start := t.addr
	end := t.addr + t.prefix
	for start < end {
		scan := pagemapScanArg{
			Size:         uint64(unsafe.Sizeof(pagemapScanArg{})),
			Start:        uint64(start),
			End:          uint64(end),
			Vec:          uint64(uintptr(unsafe.Pointer(&t.regions[0]))),
			VecLen:       uint64(len(t.regions)),
			CategoryMask: pageIsWritten,
			ReturnMask:   pageIsWritten,
		}
		count, _, errno := syscall.Syscall(
			syscall.SYS_IOCTL,
			uintptr(t.pagemap),
			pagemapScan,
			uintptr(unsafe.Pointer(&scan)),
		)
		if errno != 0 {
			return errno
		}
		if count > uintptr(len(t.regions)) {
			return fmt.Errorf("pagemap returned %d regions, capacity %d", count, len(t.regions))
		}
		for i := 0; i < int(count); i++ {
			region := t.regions[i]
			if region.Start < uint64(t.addr) || region.End > uint64(end) || region.Start >= region.End {
				return fmt.Errorf("invalid dirty page region [%#x,%#x)", region.Start, region.End)
			}
			addr := uintptr(region.Start)
			size := uintptr(region.End - region.Start)
			if err := madviseSnapshotRange(addr, size); err != nil {
				return err
			}
			if err := t.writeProtect(addr, size); err != nil {
				return err
			}
		}
		if scan.WalkEnd >= uint64(end) {
			return nil
		}
		if scan.WalkEnd <= uint64(start) {
			return errors.New("pagemap scan made no progress")
		}
		start = uintptr(scan.WalkEnd)
	}
	return nil
}

func (t *filePageSnapshotTracker) discardPrivateCOW() error {
	offset := int64((t.addr / uintptr(pageSize)) * 8)
	read := 0
	for read < len(t.pagemapBuf) {
		n, err := syscall.Pread(t.pagemap, t.pagemapBuf[read:], offset+int64(read))
		if err != nil {
			return fmt.Errorf("read pagemap: %w", err)
		}
		if n == 0 {
			return errors.New("read pagemap: unexpected EOF")
		}
		read += n
	}

	var dirtyStart uintptr
	pageCount := len(t.pagemapBuf) / 8
	for page := 0; page <= pageCount; page++ {
		dirty := false
		if page < pageCount {
			entry := binary.LittleEndian.Uint64(t.pagemapBuf[page*8:])
			dirty = entry&(pagemapPresent|pagemapSwapped) != 0 && entry&pagemapFile == 0
		}
		if dirty && dirtyStart == 0 {
			dirtyStart = t.addr + uintptr(page*pageSize)
			continue
		}
		if !dirty && dirtyStart != 0 {
			end := t.addr + uintptr(page*pageSize)
			if err := madviseSnapshotRange(dirtyStart, end-dirtyStart); err != nil {
				return err
			}
			dirtyStart = 0
		}
	}
	return nil
}

func (t *filePageSnapshotTracker) discardAll() error {
	return madviseSnapshotRange(t.addr, t.size)
}

func (t *filePageSnapshotTracker) discardHeap() error {
	if t.prefix >= t.size {
		return nil
	}
	return madviseSnapshotRange(t.addr+t.prefix, t.size-t.prefix)
}

func madviseSnapshotRange(addr, size uintptr) error {
	if _, _, errno := syscall.Syscall(
		syscall.SYS_MADVISE,
		addr,
		size,
		syscall.MADV_DONTNEED,
	); errno != 0 {
		return fmt.Errorf("wago: discard private page snapshot pages: %w", errno)
	}
	return nil
}

func (t *filePageSnapshotTracker) writeProtect(addr, size uintptr) error {
	arg := uffdioWriteProtectArg{
		Start: uint64(addr),
		Len:   uint64(size),
		Mode:  uffdWriteProtectModeWP,
	}
	if err := ioctl(t.uffd, uffdioWriteProtect, unsafe.Pointer(&arg)); err != nil {
		return fmt.Errorf("UFFDIO_WRITEPROTECT: %w", err)
	}
	return nil
}

func (t *filePageSnapshotTracker) disable() {
	t.mode.Store(dirtyTrackingFull)
	if t.pagemap >= 0 {
		_ = syscall.Close(t.pagemap)
		t.pagemap = -1
	}
	if t.uffd >= 0 {
		_ = syscall.Close(t.uffd)
		t.uffd = -1
	}
	t.pagemapBuf = nil
}

func (t *filePageSnapshotTracker) close() error {
	t.disable()
	return nil
}

func (t *filePageSnapshotTracker) selective() bool {
	return t != nil && t.mode.Load() != dirtyTrackingFull
}

//go:build linux && (amd64 || arm64)

package runtime

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	pageIsFile    = uint64(1 << 2)
	pageIsPresent = uint64(1 << 3)
	pageIsSwapped = uint64(1 << 4)
	pagemapScan   = uintptr(0xc0606610)

	dirtyRegionCapacity = 16
)

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
	start       uintptr
	end         uintptr
	enabled     bool
	fallbackErr error
}

type pageSnapshotDirtyTracker = filePageSnapshotTracker

func (s *filePageSnapshot) track(addr uintptr, size, start, end int) (pageSnapshotDirtyTracker, error) {
	t := filePageSnapshotTracker{
		snapshot: s,
		addr:     addr,
		size:     uintptr(size),
		start:    addr + uintptr(start),
		end:      addr + uintptr(end),
	}
	_, err := s.pagemapFD()
	if err != nil {
		t.fallbackErr = err
		return t, nil
	}
	t.enabled = true
	return t, nil
}

func (s *filePageSnapshot) pagemapFD() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return -1, errors.New("wago: page snapshot is closed")
	}
	if !s.pagemapTried {
		s.pagemapTried = true
		s.pagemap, s.pagemapErr = syscall.Open("/proc/self/pagemap", syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
		if s.pagemapErr != nil {
			s.pagemapErr = fmt.Errorf("open pagemap: %w", s.pagemapErr)
		}
	}
	return s.pagemap, s.pagemapErr
}

func (t *filePageSnapshotTracker) reset() error {
	t.snapshot.mu.RLock()
	defer t.snapshot.mu.RUnlock()
	if t.snapshot.closed {
		return errors.New("wago: page snapshot is closed")
	}

	if t.enabled {
		if err := t.discardPrivateCOW(); err == nil {
			return nil
		}
		t.disable()
		return t.discardAll()
	}
	return t.discardAll()
}

func (t *filePageSnapshotTracker) discardPrivateCOW() error {
	var regions [dirtyRegionCapacity]pageRegion
	start := t.start
	end := t.end
	for start < end {
		scan := pagemapScanArg{
			Size:              uint64(unsafe.Sizeof(pagemapScanArg{})),
			Start:             uint64(start),
			End:               uint64(end),
			Vec:               uint64(uintptr(unsafe.Pointer(&regions[0]))),
			VecLen:            uint64(len(regions)),
			CategoryInverted:  pageIsFile,
			CategoryMask:      pageIsFile,
			CategoryAnyOfMask: pageIsPresent | pageIsSwapped,
			ReturnMask:        0,
		}
		count, _, errno := syscall.Syscall(
			syscall.SYS_IOCTL,
			uintptr(t.snapshot.pagemap),
			pagemapScan,
			uintptr(unsafe.Pointer(&scan)),
		)
		if errno != 0 {
			return errno
		}
		if count > uintptr(len(regions)) {
			return fmt.Errorf("pagemap returned %d regions, capacity %d", count, len(regions))
		}
		for i := 0; i < int(count); i++ {
			region := regions[i]
			if region.Start < uint64(t.start) || region.End > uint64(end) || region.Start >= region.End {
				return fmt.Errorf("invalid private COW region [%#x,%#x)", region.Start, region.End)
			}
			if err := madviseSnapshotRange(uintptr(region.Start), uintptr(region.End-region.Start)); err != nil {
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

func (t *filePageSnapshotTracker) discardAll() error {
	return madviseSnapshotRange(t.addr, t.size)
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

func (t *filePageSnapshotTracker) disable() {
	t.enabled = false
}

func (t *filePageSnapshotTracker) close() error {
	t.disable()
	return nil
}

func (t *filePageSnapshotTracker) selective() bool {
	return t != nil && t.enabled
}

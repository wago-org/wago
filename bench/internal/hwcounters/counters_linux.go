//go:build linux

package hwcounters

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"golang.org/x/sys/unix"
)

type eventSpec struct {
	name     string
	typ      uint32
	config   uint64
	required bool
}

type event struct {
	eventSpec
	fd int
}

type Group struct {
	events  []event
	started bool
}

func Open() (*Group, error) {
	specs := []eventSpec{
		{name: "cycles", typ: unix.PERF_TYPE_HARDWARE, config: unix.PERF_COUNT_HW_CPU_CYCLES, required: true},
		{name: "instructions", typ: unix.PERF_TYPE_HARDWARE, config: unix.PERF_COUNT_HW_INSTRUCTIONS, required: true},
		{name: "branches", typ: unix.PERF_TYPE_HARDWARE, config: unix.PERF_COUNT_HW_BRANCH_INSTRUCTIONS},
		{name: "branch-misses", typ: unix.PERF_TYPE_HARDWARE, config: unix.PERF_COUNT_HW_BRANCH_MISSES},
		{name: "l1i-misses", typ: unix.PERF_TYPE_HW_CACHE, config: cacheConfig(unix.PERF_COUNT_HW_CACHE_L1I)},
		{name: "l1d-misses", typ: unix.PERF_TYPE_HW_CACHE, config: cacheConfig(unix.PERF_COUNT_HW_CACHE_L1D)},
		{name: "llc-misses", typ: unix.PERF_TYPE_HW_CACHE, config: cacheConfig(unix.PERF_COUNT_HW_CACHE_LL)},
		{name: "frontend-stalls", typ: unix.PERF_TYPE_HARDWARE, config: unix.PERF_COUNT_HW_STALLED_CYCLES_FRONTEND},
		{name: "backend-stalls", typ: unix.PERF_TYPE_HARDWARE, config: unix.PERF_COUNT_HW_STALLED_CYCLES_BACKEND},
	}
	group := new(Group)
	leader := -1
	for _, spec := range specs {
		attr := unix.PerfEventAttr{
			Type: spec.typ, Size: uint32(unsafe.Sizeof(unix.PerfEventAttr{})), Config: spec.config,
			Read_format: unix.PERF_FORMAT_TOTAL_TIME_ENABLED | unix.PERF_FORMAT_TOTAL_TIME_RUNNING,
			Bits:        unix.PerfBitDisabled | unix.PerfBitExcludeKernel | unix.PerfBitExcludeHv,
		}
		fd, err := unix.PerfEventOpen(&attr, 0, -1, leader, unix.PERF_FLAG_FD_CLOEXEC)
		if err != nil {
			if spec.required || !optionalEventError(err) {
				group.Close()
				return nil, fmt.Errorf("open %s counter: %w", spec.name, err)
			}
			continue
		}
		if leader < 0 {
			leader = fd
		}
		group.events = append(group.events, event{eventSpec: spec, fd: fd})
	}
	if len(group.events) < 2 {
		group.Close()
		return nil, fmt.Errorf("hardware counter group lacks required events")
	}
	return group, nil
}

func cacheConfig(cache uint64) uint64 {
	return cache | uint64(unix.PERF_COUNT_HW_CACHE_OP_READ)<<8 | uint64(unix.PERF_COUNT_HW_CACHE_RESULT_MISS)<<16
}

func optionalEventError(err error) bool {
	return errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENODEV)
}

func (g *Group) Start() error {
	if g == nil || len(g.events) == 0 || g.started {
		return fmt.Errorf("hardware counter group cannot start")
	}
	if err := unix.IoctlSetInt(g.events[0].fd, unix.PERF_EVENT_IOC_RESET, 1); err != nil {
		return fmt.Errorf("reset hardware counter group: %w", err)
	}
	if err := unix.IoctlSetInt(g.events[0].fd, unix.PERF_EVENT_IOC_ENABLE, 1); err != nil {
		return fmt.Errorf("enable hardware counter group: %w", err)
	}
	g.started = true
	return nil
}

func (g *Group) Stop() ([]Count, error) {
	if g == nil || len(g.events) == 0 || !g.started {
		return nil, fmt.Errorf("hardware counter group cannot stop")
	}
	if err := unix.IoctlSetInt(g.events[0].fd, unix.PERF_EVENT_IOC_DISABLE, 1); err != nil {
		return nil, fmt.Errorf("disable hardware counter group: %w", err)
	}
	g.started = false
	counts := make([]Count, 0, len(g.events))
	var data [24]byte
	for _, event := range g.events {
		n, err := unix.Read(event.fd, data[:])
		if err != nil {
			return nil, fmt.Errorf("read %s counter: %w", event.name, err)
		}
		if n != len(data) {
			return nil, fmt.Errorf("read %s counter: %w", event.name, io.ErrUnexpectedEOF)
		}
		counts = append(counts, Count{
			Name: event.name, Value: binary.NativeEndian.Uint64(data[0:8]),
			TimeEnabled: binary.NativeEndian.Uint64(data[8:16]), TimeRunning: binary.NativeEndian.Uint64(data[16:24]),
		})
	}
	return counts, nil
}

func (g *Group) Close() error {
	if g == nil {
		return nil
	}
	var first error
	for _, event := range g.events {
		if err := unix.Close(event.fd); err != nil && first == nil {
			first = err
		}
	}
	g.events = nil
	g.started = false
	return first
}

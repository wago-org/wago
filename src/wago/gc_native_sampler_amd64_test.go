//go:build linux && amd64 && wago_profile && wago_gc_direct_trusted_alloc

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/profile"
	"golang.org/x/sys/unix"
)

// Decode only the configured IP,TIME,PERIOD layout. Unknown records fail closed.
// head is acquired before reading bytes; caller releases tail only on success.
func gcDecodePerfRing(data []byte, tail, head uint64) (samples []profile.Sample, lost uint64, err error) {
	if len(data) == 0 || len(data)&(len(data)-1) != 0 || head < tail || head-tail > uint64(len(data)) {
		return nil, 0, fmt.Errorf("invalid ring extent")
	}
	read := func(at uint64, n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = data[(at+uint64(i))&uint64(len(data)-1)]
		}
		return b
	}
	for tail < head {
		if head-tail < 8 {
			return nil, lost, fmt.Errorf("partial header")
		}
		h := read(tail, 8)
		kind := binary.LittleEndian.Uint32(h)
		size := uint64(binary.LittleEndian.Uint16(h[6:]))
		if size < 8 || size > head-tail || size > uint64(len(data)) {
			return nil, lost, fmt.Errorf("invalid record size")
		}
		b := read(tail, int(size))
		switch kind {
		case unix.PERF_RECORD_SAMPLE:
			if size != 32 {
				return nil, lost, fmt.Errorf("invalid sample size")
			}
			s := profile.Sample{PC: binary.LittleEndian.Uint64(b[8:]), Timestamp: binary.LittleEndian.Uint64(b[16:]), Period: binary.LittleEndian.Uint64(b[24:])}
			if s.Period == 0 {
				return nil, lost, fmt.Errorf("zero sample period")
			}
			samples = append(samples, s)
		case unix.PERF_RECORD_LOST:
			if size != 24 {
				return nil, lost, fmt.Errorf("invalid lost size")
			}
			n := binary.LittleEndian.Uint64(b[16:])
			if ^uint64(0)-lost < n {
				return nil, lost, fmt.Errorf("lost overflow")
			}
			lost += n
		default:
			return nil, lost, fmt.Errorf("unsupported perf record %d", kind)
		}
		tail += size
	}
	return
}

func TestGCPerfRingDecoder(t *testing.T) {
	rec := make([]byte, 32)
	binary.LittleEndian.PutUint32(rec, unix.PERF_RECORD_SAMPLE)
	binary.LittleEndian.PutUint16(rec[6:], 32)
	binary.LittleEndian.PutUint64(rec[8:], 0x1000)
	binary.LittleEndian.PutUint64(rec[16:], 20)
	binary.LittleEndian.PutUint64(rec[24:], 10)
	for _, start := range []uint64{0, 60, 1020} {
		ring := make([]byte, 64)
		for i, b := range rec {
			ring[(start+uint64(i))%64] = b
		}
		s, l, e := gcDecodePerfRing(ring, start, start+32)
		if e != nil || l != 0 || len(s) != 1 || s[0].PC != 0x1000 || s[0].Timestamp != 20 || s[0].Period != 10 {
			t.Fatal(s, l, e)
		}
	}
	lost := make([]byte, 64)
	binary.LittleEndian.PutUint32(lost, unix.PERF_RECORD_LOST)
	binary.LittleEndian.PutUint16(lost[6:], 24)
	binary.LittleEndian.PutUint64(lost[16:], 7)
	_, n, e := gcDecodePerfRing(lost, 0, 24)
	if e != nil || n != 7 {
		t.Fatal(n, e)
	}
	for _, span := range [][2]uint64{{0, 7}, {0, 65}, {9, 8}} {
		if _, _, e := gcDecodePerfRing(lost, span[0], span[1]); e == nil {
			t.Fatal("accepted invalid span", span)
		}
	}
	bad := make([]byte, 64)
	binary.LittleEndian.PutUint32(bad, unix.PERF_RECORD_SAMPLE)
	binary.LittleEndian.PutUint16(bad[6:], 8)
	if _, _, e := gcDecodePerfRing(bad, 0, 8); e == nil {
		t.Fatal("accepted short sample")
	}
	events := []jitprofile.Event{{Sequence: 1, Timestamp: 10, Kind: "load", ImageID: 1, Image: &jitprofile.Image{ID: 1, Base: 0x1000, Size: 16, Regions: []jitprofile.Region{{Size: 16, Kind: "guest-body", Function: 0}}, Sources: []jitprofile.SourceRange{{Size: 8, Function: 0, WasmOffset: 4}}}}, {Sequence: 2, Timestamp: 30, Kind: "retire", ImageID: 1}}
	report, e := profile.Resolve(events, []profile.Sample{{PC: 0x1000, Timestamp: 9, Period: 10}, {PC: 0x1000, Timestamp: 20, Period: 10}, {PC: 0x1000, Timestamp: 31, Period: 10}}, "nanoseconds")
	if e != nil || report.UnknownSamples != 2 || report.Samples != 3 {
		t.Fatal(report, e)
	}
}

func TestGCNativeStructSampler(t *testing.T) {
	out := os.Getenv("GC_NATIVE_SAMPLE_OUT")
	if out == "" {
		t.Skip("set GC_NATIVE_SAMPLE_OUT only after independent review")
	}
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	raw, err := os.ReadFile("../../bench/experiments/gc-v8/gc-struct-allocation-access.wasm")
	if err != nil {
		t.Fatal(err)
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false)
	plain, err := Compile(cfg, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	for repeat := 0; repeat < 2; repeat++ {
		session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: true})
		defer session.Close()
		rt := NewRuntime(WithRuntimeConfig(cfg.WithCodeProfile(session)))
		defer rt.Close()
		mod, err := rt.Compile(raw)
		if err != nil {
			t.Fatal(err)
		}
		defer mod.Close()
		if !bytes.Equal(plain.code, mod.c.code) {
			t.Fatal("source maps changed struct bytes")
		}
		config := GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 2 << 20, ThroughputHeapBytes: 8 << 20}
		in, err := rt.Instantiate(context.Background(), mod, WithGC(config))
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if in.gcDirectView == nil || !bytes.Equal(in.c.codeCache.mem[:len(mod.c.code)], plain.code) {
			t.Fatal("loaded native parity/admission failed")
		}
		call := func() {
			got, e := in.Invoke("performance", 4096)
			if e != nil || len(got) != 1 || got[0] != 536739840 {
				t.Fatal(got, e)
			}
		}
		for i := 0; i < 100; i++ {
			call()
		}
		attr := unix.PerfEventAttr{Type: unix.PERF_TYPE_SOFTWARE, Size: uint32(unsafe.Sizeof(unix.PerfEventAttr{})), Config: unix.PERF_COUNT_SW_CPU_CLOCK, Sample: 10000000, Sample_type: unix.PERF_SAMPLE_IP | unix.PERF_SAMPLE_TIME | unix.PERF_SAMPLE_PERIOD, Bits: unix.PerfBitDisabled | unix.PerfBitExcludeKernel | unix.PerfBitExcludeHv | unix.PerfBitUseClockID, Clockid: unix.CLOCK_MONOTONIC, Wakeup: 1}
		fd, e := unix.PerfEventOpen(&attr, 0, -1, -1, unix.PERF_FLAG_FD_CLOEXEC)
		if e != nil {
			t.Fatal("sample event open", e)
		}
		defer unix.Close(fd)
		page := os.Getpagesize()
		mem, e := unix.Mmap(fd, 0, page*65, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if e != nil {
			t.Fatal(e)
		}
		defer unix.Munmap(mem)
		meta := (*unix.PerfEventMmapPage)(unsafe.Pointer(&mem[0]))
		offset, size := meta.Data_offset, meta.Data_size
		if offset < uint64(page) || offset > uint64(len(mem)) || size > uint64(len(mem))-offset || size == 0 || size&(size-1) != 0 {
			t.Fatal("invalid mmap data extent")
		}
		before := in.gc.Stats()
		start := jitprofile.Now()
		if e = unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_ENABLE, 0); e != nil {
			t.Fatal(e)
		}
		calls := uint64(0)
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			call()
			calls++
		}
		if e = unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_DISABLE, 0); e != nil {
			t.Fatal(e)
		}
		end := jitprofile.Now()
		after := in.gc.Stats()
		head := atomic.LoadUint64(&meta.Data_head)
		tail := atomic.LoadUint64(&meta.Data_tail)
		samples, lost, e := gcDecodePerfRing(mem[offset:offset+size], tail, head)
		if e != nil {
			t.Fatal(e)
		}
		atomic.StoreUint64(&meta.Data_tail, head)
		for _, s := range samples {
			if s.Timestamp < start || s.Timestamp > end {
				t.Fatal("sample outside monotonic capture interval")
			}
		}
		in.Close()
		mod.Close()
		rt.Close()
		events, status := session.Read(0)
		report, e := profile.Resolve(events, samples, "nanoseconds")
		if e != nil {
			t.Fatal(e)
		}
		goNames := map[string]uint64{}
		var totalWeight, goWeight uint64
		unknown := uint64(0)
		for _, s := range samples {
			totalWeight += s.Period
			if f := runtime.FuncForPC(uintptr(s.PC)); f != nil {
				goNames[f.Name()] += s.Period
				goWeight += s.Period
			} else {
				unknown += s.Period
			}
		}
		receipt := map[string]any{"artifact_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "native_sha256": fmt.Sprintf("%x", sha256.Sum256(plain.code)), "go_version": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "total_sample_weight_ns": totalWeight, "native_resolved_weight_ns": report.Weight - report.UnknownWeight, "native_unresolved_weight_ns": report.UnknownWeight, "go_symbol_weight_total_ns": goWeight, "repeat": repeat, "start_ns": start, "end_ns": end, "calls": calls, "before": before, "after": after, "config": config, "period_ns": 10000000, "thread_id": unix.Gettid(), "samples": samples, "lost": lost, "journal_status": status, "events": events, "resolved": report, "go_symbol_weight_ns": goNames, "non_go_or_unknown_weight_ns": unknown, "scope": "self-thread user CPU only; other Go threads excluded; native gaps unknown; no benchmark speed claim"}
		b, marshalErr := json.MarshalIndent(receipt, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if e = os.WriteFile(filepath.Join(out, fmt.Sprintf("repeat%d.json", repeat)), b, 0644); e != nil {
			t.Fatal(e)
		}
		if len(samples) == 0 || lost != 0 || status.Dropped != 0 || status.Clock != "monotonic" {
			t.Fatal("incomplete capture", len(samples), lost, status)
		}
		if after.Allocations-before.Allocations != calls*262208 {
			t.Fatal("allocation count mismatch")
		}
	}
}

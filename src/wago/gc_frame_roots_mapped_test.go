package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// mappedFrameChain places the terminal return word at the mapping's last eight bytes.
func mappedFrameChain(t testing.TB, frames int) gcNativeFrameRoots {
	t.Helper()
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		t.Skip("native engine unavailable")
	}
	eng, err := coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := eng.Close(); err != nil {
			t.Error(err)
		}
	})
	const stride = 32 + shared.ARM64FrameRecordBytes
	if frames <= 0 || uintptr(frames) > uintptr(eng.StackBytes())/stride {
		t.Fatal("invalid mapped fixture size")
	}
	base := eng.StackTop() - uintptr(frames*stride)
	frame := unsafe.Slice((*byte)(offHeapPtr(base)), frames*stride)
	// Return PCs are metadata values; this fixture does not execute or read code.
	const codeBase = uintptr(0x1000)
	for i := 0; i < frames; i++ {
		binary.LittleEndian.PutUint64(frame[i*stride+16:], uint64(i+1))
		ret := codeBase + 100
		if i == frames-1 {
			ret = codeBase + 200
		}
		binary.LittleEndian.PutUint64(frame[i*stride+32+shared.ARM64SavedLROffset:], uint64(ret))
	}
	return gcNativeFrameRoots{
		owner: &Instance{eng: eng}, base: base, offsets: []uint32{16}, frameBytes: 32,
		frameLayout: gcNativeFrameLayoutARM64, codeBase: codeBase, codeBytes: 256,
		adapterReturnOffsets: []uint32{200},
		callsites:            []compiledGCFrameCallsite{{returnOffset: 100, frameBytes: 32, offsets: []uint32{16}}},
	}
}

func TestGCNativeFrameRootsDeepMappedStack(t *testing.T) {
	const frames = 4098
	roots := mappedFrameChain(t, frames)
	seen := 0
	roots.RangeRoots(func(slot gc.RootSlot) bool {
		seen++
		if got := uint32(slot.GetRef()); got != uint32(seen) {
			t.Fatalf("root %d = %d", seen, got)
		}
		slot.SetRef(slot.GetRef() + 1)
		return true
	})
	if seen != frames {
		t.Fatalf("visited %d roots, want %d", seen, frames)
	}
	sink := new(gcCountingRootRefSink)
	if !roots.RangeRootRefs(sink) || sink.count != frames || sink.sum != uint64(frames*(frames+3)/2) {
		t.Fatalf("rewritten roots count/sum = %d/%d", sink.count, sink.sum)
	}
	sink.count, sink.sum, sink.stopAfter = 0, 0, 3
	if roots.RangeRootRefs(sink) || sink.count != 3 {
		t.Fatal("visitor cancellation did not stop traversal")
	}
}

func TestGCNativeFrameRootsMappedStackBounds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		frameBytes uint32
		want       string
	}{
		{name: "frame", frameBytes: 32 + shared.ARM64FrameRecordBytes + 1, want: "generic GC native frame exceeds stack bounds"},
		{name: "return word", frameBytes: 33, want: "generic GC native return address exceeds stack bounds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roots := mappedFrameChain(t, 1)
			roots.frameBytes = tc.frameBytes
			defer func() {
				if got := recover(); got == nil || fmt.Sprint(got) != tc.want {
					t.Errorf("panic = %v, want %q", got, tc.want)
				}
			}()
			roots.RangeRoots(func(gc.RootSlot) bool { return true })
		})
	}
}

func BenchmarkGCNativeFrameRootsMappedStack(b *testing.B) {
	for _, frames := range []int{1, 64, 4098} {
		b.Run(fmt.Sprintf("frames=%d", frames), func(b *testing.B) {
			roots := mappedFrameChain(b, frames)
			sink := new(gcCountingRootRefSink)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sink.count, sink.sum = 0, 0
				if !roots.RangeRootRefs(sink) {
					b.Fatal("walk stopped")
				}
			}
			b.StopTimer()
			if sink.count != frames || sink.sum != uint64(frames*(frames+1)/2) {
				b.Fatal("incomplete walk")
			}
		})
	}
}

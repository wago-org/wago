//go:build linux && amd64 && !tinygo && !wago_guardpage && !wago_no_gc

package wago

import (
	"encoding/binary"
	"fmt"
	goruntime "runtime"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCNativeFrameRootAdapterWritesParkedSlot(t *testing.T) {
	frame := make([]byte, 16)
	binary.LittleEndian.PutUint64(frame[8:], 7)
	roots := gcNativeFrameRoots{base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{8}}
	seen := 0
	roots.RangeRoots(func(slot gc.RootSlot) bool {
		seen++
		if got := slot.GetRef(); got != gc.Ref(7) {
			t.Fatalf("root = %d, want 7", got)
		}
		slot.SetRef(gc.Ref(11))
		return true
	})
	goruntime.KeepAlive(frame)
	if seen != 1 || binary.LittleEndian.Uint64(frame[8:]) != 11 {
		t.Fatalf("root adapter seen=%d frame=%#x, want one rewritten qword", seen, frame[8:])
	}
}

func TestGCNativeFrameRootAdapterClassifiesFrames(t *testing.T) {
	frame := make([]byte, 16)
	binary.LittleEndian.PutUint64(frame[8:], 7)
	roots := gcNativeFrameRoots{base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{8}}
	counter := new(gcClassifiedRootCounter)
	roots.RangeClassifiedRootRefs(counter)
	goruntime.KeepAlive(frame)
	if counter.classes[gc.RootNativeFrame] != 1 || len(counter.classes) != 1 {
		t.Fatalf("classified native roots = %v", counter.classes)
	}
}

func BenchmarkGCNativeFrameRootMetadataWidths(b *testing.B) {
	if !requireCompilerDiagnostics(b) {
		return
	}
	for _, roots := range []uint32{64, 65, 128, 256, 1024} {
		b.Run(fmt.Sprintf("roots=%d", roots), func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithGCCodeTelemetry(true), gcFrameRootLimitModule(roots))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			telemetry, ok := compiled.GCNativeCodeTelemetry()
			if !ok || telemetry.RootMapBytes == 0 {
				b.Fatalf("native code telemetry = %+v, enabled=%v", telemetry, ok)
			}
			plan := compiled.genericGCFrameRoots()
			if plan == nil || len(plan.safepoints) != 1 {
				b.Fatalf("root plan = %+v", plan)
			}
			b.ReportAllocs()
			var sink *compiledGCFrameSafepoint
			b.ResetTimer()
			b.ReportMetric(float64(telemetry.RootMapBytes), "root-map-bytes/site")
			for i := 0; i < b.N; i++ {
				sink = plan.safepointByID(1)
			}
			if sink == nil {
				b.Fatal("safepoint lookup failed")
			}
		})
	}
}

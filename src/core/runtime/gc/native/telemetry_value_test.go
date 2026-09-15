package gc

import (
	"reflect"
	"runtime"
	"testing"
)

func TestTelemetryValueAggregationAndMemoryAttribution(t *testing.T) {
	paths := PathTelemetry{
		NativeFastAllocations:  1,
		GoAllocationPaths:      2,
		GoHelperTransitions:    3,
		ConditionalMediumPaths: 4,
		CardGrowths:            5,
		HandleRefills:          6,
		NurseryExhaustions:     7,
		MinorCollections:       8,
		FullCollections:        9,
		BackingGrowths:         10,
		BackingBytesCopied:     11,
	}
	var gotPaths PathTelemetry
	gotPaths.Add(paths)
	if !reflect.DeepEqual(gotPaths, paths) {
		t.Fatalf("path aggregation = %+v, want %+v", gotPaths, paths)
	}
	var nilPaths *PathTelemetry
	nilPaths.Add(paths)

	native := NativeCodeTelemetry{
		TotalBytes:            1,
		AllocationBytes:       2,
		HandleResolutionBytes: 3,
		TypeCastBytes:         4,
		NullCheckBytes:        5,
		BoundsCheckBytes:      6,
		BarrierBytes:          7,
		SpillReloadBytes:      8,
		HelperCallBytes:       9,
		SharedStubBytes:       10,
		TrapStubBytes:         11,
		RootMapBytes:          12,
	}
	var gotNative NativeCodeTelemetry
	gotNative.Add(native)
	if !reflect.DeepEqual(gotNative, native) {
		t.Fatalf("native-code aggregation = %+v, want %+v", gotNative, native)
	}
	var nilNative *NativeCodeTelemetry
	nilNative.Add(native)

	memory := CaptureMemoryDomains(13, 14, ManagedHeapTelemetry{CommittedBytes: 15})
	if memory.GoCompilerHeapBytes != 13 || memory.ExecutableJITBytes != 14 || memory.WasmManagedBytes != 15 {
		t.Fatalf("memory attribution = %+v", memory)
	}
	report := NewBenchmarkTelemetryReport("coverage")
	if report.SchemaVersion != TelemetrySchemaVersion || report.Name != "coverage" || report.GOOS != runtime.GOOS || report.GOARCH != runtime.GOARCH || report.GoVersion == "" {
		t.Fatalf("benchmark report identity = %+v", report)
	}
}

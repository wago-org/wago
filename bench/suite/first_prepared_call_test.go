package wagobench

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

const firstPreparedBatchSize = 16

var firstPreparedDiagnostics = flag.Bool("wago.bench.first-prepared", false, "enable bounded first prepared-call lifecycle diagnostics; requires -benchtime=Nx with N<=256")

// The guest increments a work counter at 0 and stores/returns (input XOR 90)+count.
// Host memory reads observe fresh state without warming a guest entry point.
func firstPreparedModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x7f, 1, 0x7f})),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(5, []byte{1, 1, 1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0, 0x41, 0, 0x28, 2, 0, 0x41, 1, 0x6a, 0x36, 2, 0,
			0x41, 4, 0x20, 0, 0x41, 0xda, 0, 0x73, 0x41, 0, 0x28, 2, 0, 0x6a, 0x36, 2, 0,
			0x41, 4, 0x28, 2, 0, 0x0b,
		}))),
	)
}

type firstPreparedBatch struct {
	instances [firstPreparedBatchSize]*wago.Instance
	functions [firstPreparedBatchSize]*wago.WasmFunc
	results   [firstPreparedBatchSize]uint64
}

func newFirstPreparedBatch(c *wago.Compiled) (*firstPreparedBatch, error) {
	batch := new(firstPreparedBatch)
	for i := range batch.instances {
		in, err := wago.Instantiate(c, wago.InstantiateOptions{})
		if err != nil {
			_ = batch.close()
			return nil, err
		}
		batch.instances[i] = in
		batch.functions[i], err = in.WasmFunc("run")
		if err != nil {
			_ = batch.close()
			return nil, err
		}
	}
	return batch, nil
}

func (batch *firstPreparedBatch) close() error {
	var first error
	for i, in := range batch.instances {
		if in == nil {
			continue
		}
		if err := in.Close(); err != nil && first == nil {
			first = err
		}
		batch.instances[i] = nil
	}
	return first
}

func (batch *firstPreparedBatch) invoke() error {
	for i, fn := range batch.functions {
		got, err := fn.Invoke(uint64(i*17 + 7))
		if err != nil {
			return err
		}
		if len(got) != 1 {
			return fmt.Errorf("instance %d returned %d results, want 1", i, len(got))
		}
		// Copy before a later invocation can reuse its instance's result storage.
		batch.results[i] = got[0]
	}
	return nil
}

func (batch *firstPreparedBatch) check(wantCalls uint32) error {
	for i, in := range batch.instances {
		count, ok := in.ReadUint32Le(0)
		if !ok || count != wantCalls {
			return fmt.Errorf("instance %d completed calls = %d (read=%t), want %d", i, count, ok, wantCalls)
		}
		want := uint32(0)
		if wantCalls != 0 {
			want = (uint32(i*17+7) ^ 90) + wantCalls
		}
		value, ok := in.ReadUint32Le(4)
		if !ok || value != want {
			return fmt.Errorf("instance %d guest value = %d (read=%t), want %d", i, value, ok, want)
		}
		if batch.results[i] != uint64(want) {
			return fmt.Errorf("instance %d returned value = %d, want %d", i, batch.results[i], want)
		}
	}
	return nil
}

func firstPreparedIterationLimit(value string) error {
	if !strings.HasSuffix(value, "x") {
		return fmt.Errorf("first prepared-call diagnostics require -benchtime=Nx with 1<=N<=256; got %q", value)
	}
	n, err := strconv.Atoi(strings.TrimSuffix(value, "x"))
	if err != nil || n < 1 || n > 256 {
		return fmt.Errorf("first prepared-call iteration count must be 1..256; got %q", value)
	}
	return nil
}

func TestFirstPreparedIterationLimit(t *testing.T) {
	for _, value := range []string{"1x", "64x", "256x"} {
		if err := firstPreparedIterationLimit(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"1s", "100ms", "0x", "257x", "-1x", "badx", ""} {
		if err := firstPreparedIterationLimit(value); err == nil {
			t.Fatalf("accepted unbounded/invalid iteration setting %q", value)
		}
	}
}

func TestFirstPreparedCallState(t *testing.T) {
	c, err := wago.Compile(nil, firstPreparedModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for round := 0; round < 3; round++ {
		batch, err := newFirstPreparedBatch(c)
		if err != nil {
			t.Fatal(err)
		}
		for calls := uint32(0); calls <= 3; calls++ {
			if calls != 0 {
				if err := batch.invoke(); err != nil {
					_ = batch.close()
					t.Fatal(err)
				}
			}
			if err := batch.check(calls); err != nil {
				_ = batch.close()
				t.Fatal(err)
			}
		}
		if err := batch.close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFirstPreparedCallControls(t *testing.T) {
	c, err := wago.Compile(nil, firstPreparedModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, control := range []string{"warmed", "reused", "duplicate-handle", "wrong-result"} {
		t.Run(control, func(t *testing.T) {
			batch, err := newFirstPreparedBatch(c)
			if err != nil {
				t.Fatal(err)
			}
			defer batch.close()
			if err := batch.check(0); err != nil {
				t.Fatal(err)
			}
			wantCalls, category := uint32(1), "completed calls"
			if control == "duplicate-handle" {
				batch.functions[1] = batch.functions[0]
			}
			if err := batch.invoke(); err != nil {
				t.Fatal(err)
			}
			switch control {
			case "warmed":
				wantCalls = 0
			case "reused":
				if err := batch.invoke(); err != nil {
					t.Fatal(err)
				}
			case "wrong-result":
				batch.results[3] ^= 1
				category = "returned value"
			}
			if err := batch.check(wantCalls); err == nil || !strings.Contains(err.Error(), category) {
				t.Fatalf("%s: got %v, want %q rejection", control, err, category)
			}
		})
	}
}

// Each operation has 16 distinct instances and exactly one timed call per
// instance. All standard metrics remain per batch; ns/call is also reported.
func BenchmarkFirstPreparedCall(b *testing.B) {
	if !*firstPreparedDiagnostics {
		b.Skip("enable with -wago.bench.first-prepared -benchtime=64x")
	}
	if err := firstPreparedIterationLimit(flag.Lookup("test.benchtime").Value.String()); err != nil {
		b.Fatal(err)
	}
	for _, phase := range []string{"first", "second", "lifecycle"} {
		b.Run(phase, func(b *testing.B) {
			b.StopTimer()
			raw := firstPreparedModule()
			c, err := wago.Compile(nil, raw)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			b.Logf("fixture_sha256=%x wasm_bytes=%d functions=1 native_bytes=%d compiled_reuse=true", sha256.Sum256(raw), len(raw), c.CodeSize())
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if phase == "lifecycle" {
					b.StartTimer()
				}
				batch, err := newFirstPreparedBatch(c)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				if err := batch.check(0); err != nil {
					_ = batch.close()
					b.Fatal(err)
				}
				wantCalls := uint32(1)
				if phase == "second" {
					if err := batch.invoke(); err != nil {
						_ = batch.close()
						b.Fatal(err)
					}
					if err := batch.check(1); err != nil {
						_ = batch.close()
						b.Fatal(err)
					}
					batch.results = [firstPreparedBatchSize]uint64{}
					wantCalls = 2
				}
				b.StartTimer()
				err = batch.invoke()
				b.StopTimer()
				if err == nil {
					err = batch.check(wantCalls)
				}
				if err != nil {
					_ = batch.close()
					b.Fatal(err)
				}
				if phase == "lifecycle" {
					b.StartTimer()
				}
				err = batch.close()
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(firstPreparedBatchSize, "calls/op")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/(float64(b.N)*firstPreparedBatchSize), "ns/call")
			b.ReportMetric(float64(c.CodeSize()), "native-B")
			b.ReportMetric(float64(len(raw)), "wasm-B")
			b.ReportMetric(firstPreparedBatchSize*65536, "linear-capacity-B")
		})
	}
}

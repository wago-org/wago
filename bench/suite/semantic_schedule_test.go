package wagobench

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/bench/internal/semanticcorpus"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The guest records completed calls and their three arguments in a bounded
// ring. Pointer-export setup has separate counters at offsets 4 and 8.
func semanticScheduleModule() []byte {
	constant := func(v int32) []byte { return append([]byte{0x41}, wasmtest.SLEB32(v)...) }
	increment := func(offset int32) []byte {
		body := append(constant(offset), constant(offset)...)
		return append(body, 0x28, 2, 0, 0x41, 1, 0x6a, 0x36, 2, 0)
	}
	var run []byte
	for arg := byte(0); arg < 3; arg++ {
		run = append(run, constant(128+int32(arg)*4)...)
		run = append(run, 0x41, 0, 0x28, 2, 0, 0x41, 15, 0x71, 0x41, 12, 0x6c, 0x6a)
		run = append(run, 0x20, arg, 0x36, 2, 0)
	}
	run = append(run, increment(0)...)
	run = append(run, 0x0b)
	input := append(increment(4), constant(1024)...)
	output := append(increment(8), constant(2048)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 3, 0x7f, 0x7f, 0x7f, 0}, []byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, []byte{3, 0, 1, 1}),
		wasmtest.Section(5, []byte{1, 1, 1, 1}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("input", 0, 1),
			wasmtest.ExportEntry("output", 0, 2), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(run),
			wasmtest.Code(append(input, 0x0b)), wasmtest.Code(append(output, 0x0b)))),
	)
}

type semanticScheduleFixture struct {
	name     string
	module   semanticcorpus.Module
	want     [][3]uint32
	pointers bool
}

func semanticScheduleFixtures() []semanticScheduleFixture {
	scalar := semanticcorpus.Module{Invoke: semanticcorpus.Invoke{Export: "run", Args: []int32{-7, 19, -1}}}
	vectors := func(pointers bool) semanticcorpus.Module {
		v := &semanticcorpus.Vectors{InputOffset: 512, OutputOffset: 768, Mod: 7,
			Cases: []semanticcorpus.VectorCase{{Len: 0}, {Len: 3}, {Len: 17}}}
		if pointers {
			v.InputPtrExport, v.OutputPtrExport = "input", "output"
		}
		return semanticcorpus.Module{Invoke: semanticcorpus.Invoke{Export: "run", Vectors: v}}
	}
	return []semanticScheduleFixture{
		{"scalar", scalar, [][3]uint32{{0xfffffff9, 19, 0xffffffff}}, false},
		{"vector-offsets", vectors(false), [][3]uint32{{512, 0, 768}, {512, 3, 768}, {512, 17, 768}}, false},
		{"vector-pointers", vectors(true), [][3]uint32{{1024, 0, 2048}, {1024, 3, 2048}, {1024, 17, 2048}}, true},
	}
}

type semanticScheduleAdapter struct {
	invoke func() error
	read   func(uint32, uint32) ([]byte, bool)
	mutate func(func([][]uint64) [][]uint64)
}

// Use the existing prepared adapters. This adds no alternate invocation loop.
func newSemanticScheduleAdapter(tb testing.TB, engine string, fixture semanticScheduleFixture) semanticScheduleAdapter {
	tb.Helper()
	raw := semanticScheduleModule()
	if engine == "wago" {
		c, err := wago.Compile(nil, raw)
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = c.Close() })
		in, err := wago.Instantiate(c, wago.InstantiateOptions{})
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = in.Close() })
		prepared, err := prepareWagoSemanticExec(in, fixture.module)
		if err != nil {
			tb.Fatal(err)
		}
		return semanticScheduleAdapter{prepared.invoke, in.Read, func(f func([][]uint64) [][]uint64) { prepared.calls = f(prepared.calls) }}
	}
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	tb.Cleanup(func() { _ = r.Close(ctx) })
	in, err := r.Instantiate(ctx, raw)
	if err != nil {
		tb.Fatal(err)
	}
	prepared, err := prepareWazeroSemanticExec(ctx, in, fixture.module)
	if err != nil {
		tb.Fatal(err)
	}
	return semanticScheduleAdapter{func() error { return prepared.invoke(ctx) }, in.Memory().Read,
		func(f func([][]uint64) [][]uint64) { prepared.calls = f(prepared.calls) }}
}

func checkSemanticSchedule(a semanticScheduleAdapter, fixture semanticScheduleFixture, operations uint64) error {
	data, ok := a.read(0, 320)
	if !ok {
		return fmt.Errorf("cannot read guest schedule")
	}
	wantCalls := operations * uint64(len(fixture.want))
	if got := binary.LittleEndian.Uint32(data); got != uint32(wantCalls) {
		return fmt.Errorf("completed calls = %d, want %d", got, wantCalls)
	}
	wantPointers := uint32(0)
	if fixture.pointers {
		wantPointers = 1
	}
	for _, offset := range []int{4, 8} {
		if got := binary.LittleEndian.Uint32(data[offset:]); got != wantPointers {
			return fmt.Errorf("pointer setup at %d = %d, want %d", offset, got, wantPointers)
		}
	}
	first := uint64(0)
	if wantCalls > 16 {
		first = wantCalls - 16
	}
	for call := first; call < wantCalls; call++ {
		for arg, want := range fixture.want[call%uint64(len(fixture.want))] {
			offset := 128 + int(call%16)*12 + arg*4
			if got := binary.LittleEndian.Uint32(data[offset:]); got != want {
				return fmt.Errorf("call %d argument %d = %#x, want %#x", call, arg, got, want)
			}
		}
	}
	return nil
}

func TestPreparedSemanticSchedule(t *testing.T) {
	for _, engine := range []string{"wago", "wazero"} {
		for _, fixture := range semanticScheduleFixtures() {
			t.Run(engine+"/"+fixture.name, func(t *testing.T) {
				a := newSemanticScheduleAdapter(t, engine, fixture)
				if err := checkSemanticSchedule(a, fixture, 0); err != nil {
					t.Fatal(err)
				}
				if fixture.module.Invoke.Vectors != nil {
					data, ok := a.read(fixture.want[0][0], 17)
					if !ok {
						t.Fatal("input not readable")
					}
					for i, got := range data {
						if got != byte(i%7) {
							t.Fatalf("input byte %d = %d, want %d", i, got, i%7)
						}
					}
				}
				for operation := uint64(1); operation <= 9; operation++ {
					if err := a.invoke(); err != nil {
						t.Fatal(err)
					}
					if err := checkSemanticSchedule(a, fixture, operation); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestPreparedSemanticScheduleControls(t *testing.T) {
	fixture := semanticScheduleFixtures()[2]
	controls := []struct {
		name   string
		want   string
		mutate func([][]uint64) [][]uint64
	}{
		{"omitted", "completed calls", func(c [][]uint64) [][]uint64 { return c[:2] }},
		{"duplicated", "completed calls", func(c [][]uint64) [][]uint64 { return append(c, c[0]) }},
		{"reordered", "argument", func(c [][]uint64) [][]uint64 { c[0], c[1] = c[1], c[0]; return c }},
		{"wrong-argument", "argument", func(c [][]uint64) [][]uint64 { c[1][1]++; return c }},
		{"same-count-duplicate", "argument", func(c [][]uint64) [][]uint64 { c[1] = c[0]; return c }},
	}
	for _, engine := range []string{"wago", "wazero"} {
		for _, control := range controls {
			t.Run(engine+"/"+control.name, func(t *testing.T) {
				a := newSemanticScheduleAdapter(t, engine, fixture)
				if err := checkSemanticSchedule(a, fixture, 0); err != nil {
					t.Fatal(err)
				}
				a.mutate(control.mutate)
				if err := a.invoke(); err != nil {
					t.Fatal(err)
				}
				if err := checkSemanticSchedule(a, fixture, 1); err == nil || !strings.Contains(err.Error(), control.want) {
					t.Fatalf("wrong schedule: got %v, want %q", err, control.want)
				}
			})
		}
	}
}

func BenchmarkPreparedSemanticSchedule(b *testing.B) {
	for _, engine := range []string{"wago", "wazero"} {
		for _, fixture := range semanticScheduleFixtures() {
			b.Run(engine+"/"+fixture.name, func(b *testing.B) {
				a := newSemanticScheduleAdapter(b, engine, fixture)
				if err := a.invoke(); err != nil {
					b.Fatal(err)
				}
				if err := checkSemanticSchedule(a, fixture, 1); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := a.invoke(); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if err := checkSemanticSchedule(a, fixture, uint64(b.N)+1); err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(fixture.want)), "guest-calls/op")
			})
		}
	}
}

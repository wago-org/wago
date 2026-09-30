//go:build !windows

package wagobench

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"github.com/wago-org/wasi/p1"
)

func wasiHostLoopModule() []byte {
	imp := append(wasmtest.Name(p1.Module), wasmtest.Name("args_sizes_get")...)
	imp = append(imp, 0, 0)
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x41, 0, 0x41, 4, 0x10, 0, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 2, 0x7f, 0x7f, 1, 0x7f}, []byte{0x60, 1, 0x7f, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(5, []byte{1, 1, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func BenchmarkWASIHostCallDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	c, err := wago.Compile(nil, wasiHostLoopModule())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for _, count := range []uint64{1, 1024} {
		for _, lifecycle := range []bool{false, true} {
			b.Run(fmt.Sprintf("calls=%d/lifecycle=%t", count, lifecycle), func(b *testing.B) {
				makeInstance := func() *wago.Instance {
					in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: p1.Imports(p1.Config{Args: []string{"host-control", "one"}})})
					if err != nil {
						b.Fatal(err)
					}
					return in
				}
				in := makeInstance()
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					b.Fatal(err)
				}
				result, err := fn.Invoke(count)
				if err != nil || len(result) != 1 || result[0] != 0 || binary.LittleEndian.Uint32(in.Memory().UnsafeBytes()) != 2 {
					b.Fatalf("host oracle: %v %v", result, err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if lifecycle {
						next := makeInstance()
						result, err = next.Invoke("run", count)
						closeErr := next.Close()
						if closeErr != nil {
							b.Fatal(closeErr)
						}
					} else {
						result, err = fn.Invoke(count)
					}
					if err != nil || len(result) != 1 || result[0] != 0 {
						b.Fatalf("host call: %v %v", result, err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(count), "host-calls/op")
			})
		}
	}
}

func TestWASIConstructionCopiesConfiguration(t *testing.T) {
	c, err := wago.Compile(nil, lifecycleStateModule(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			argument := fmt.Sprintf("argument-%d", i)
			environment := fmt.Sprintf("KEY=value-%d", i)
			cfg := p1.Config{Args: []string{argument}, Env: []string{environment}, Stdin: bytes.NewBufferString(argument)}
			imports := p1.Imports(cfg)
			cfg.Args[0] = "mutated"
			cfg.Env[0] = "OTHER=mutated"
			in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, tc := range []struct{ name, want string }{{"args_get", argument}, {"environ_get", environment}} {
				got, err := in.Invoke(tc.name, 16, 64)
				if err != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("%s: %v %v", tc.name, got, err)
				}
				mem := in.Memory().UnsafeBytes()
				if !bytes.Equal(mem[64:64+len(tc.want)], []byte(tc.want)) || mem[64+len(tc.want)] != 0 {
					t.Fatalf("%s: caller mutation reached command", tc.name)
				}
			}
		})
	}
}

func TestWASIRegistrationCopiesSignatures(t *testing.T) {
	m := minimalWASICommand()
	c, err := wago.Compile(nil, m.bytes)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	params := []wago.ValType{wago.ValI32, wago.ValI32}
	results := []wago.ValType{wago.ValI32}
	imports := wago.NewImports()
	imports.HostFunc(p1.Module, "args_sizes_get", wago.CallerHostCallFunc(func(_ wago.Caller, call wago.HostCall) { call.ResultSlots()[0] = 0 })).Params(params...).Results(results...)
	params[0] = wago.ValI64
	results[0] = wago.ValI64
	in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run"); err != nil {
		t.Fatal(err)
	}
}

func FuzzWASIConstructionIsolation(f *testing.F) {
	for _, s := range []string{"", "argument", "a\x00b", "世界"} {
		f.Add(s)
	}
	c, err := wago.Compile(nil, minimalWASICommand().bytes)
	if err != nil {
		f.Fatal(err)
	}
	defer c.Close()
	f.Fuzz(func(t *testing.T, arg string) {
		if len(arg) > 256 {
			t.Skip()
		}
		cfg := p1.Config{Args: []string{arg}}
		imports := p1.Imports(cfg)
		cfg.Args[0] = "changed after construction"
		in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		result, err := in.Invoke("run")
		if err != nil || result[0] != 0 {
			t.Fatalf("%v %v", result, err)
		}
		mem := in.Memory().UnsafeBytes()
		if binary.LittleEndian.Uint32(mem) != 1 || binary.LittleEndian.Uint32(mem[4:]) != uint32(len(arg)+1) {
			t.Fatalf("configuration crossed bundle boundary: %v", mem[:8])
		}
	})
}

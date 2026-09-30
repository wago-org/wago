//go:build !windows

package wagobench

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"github.com/wago-org/wasi/p1"
	"io"
	"testing"
)

func wasiFileLoopModule() []byte {
	imp := append(wasmtest.Name(p1.Module), wasmtest.Name("fd_write")...)
	imp = append(imp, 0, 0)
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b}
	data := make([]byte, 17)
	binary.LittleEndian.PutUint32(data, 16)
	binary.LittleEndian.PutUint32(data[4:], 1)
	data[16] = 'x'
	segment := append([]byte{0, 0x41, 0, 0x0b, byte(len(data))}, data...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f}, []byte{0x60, 1, 0x7f, 1, 0x7f})),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(5, []byte{1, 1, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))), wasmtest.Section(11, wasmtest.Vec(segment)))
}

func BenchmarkWASIFileCallDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	c, err := wago.Compile(nil, wasiFileLoopModule())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	for _, count := range []uint64{1, 1024} {
		b.Run(fmt.Sprintf("calls=%d", count), func(b *testing.B) {
			in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: p1.Imports(p1.Config{Stdout: io.Discard})})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				b.Fatal(err)
			}
			got, err := fn.Invoke(count)
			if err != nil || len(got) != 1 || got[0] != 0 || binary.LittleEndian.Uint32(in.Memory().UnsafeBytes()[8:]) != 1 {
				b.Fatalf("write oracle: %v %v", got, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err = fn.Invoke(count)
				if err != nil || len(got) != 1 || got[0] != 0 {
					b.Fatalf("write: %v %v", got, err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(count), "host-calls/op")
		})
	}
}

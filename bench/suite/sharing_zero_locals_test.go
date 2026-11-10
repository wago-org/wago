package wagobench

import (
	"crypto/sha256"
	"fmt"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func sharingZeroLocalsModule(wide bool, locals int) []byte {
	typ := wasm.I32
	if wide {
		typ = wasm.I64
	}
	body := append([]byte{1}, wasmtest.ULEB(uint32(locals))...)
	body = append(body, wasm.MustEncodeValType(typ), 0x20)
	body = append(body, wasmtest.ULEB(uint32(locals-1))...)
	body = append(body, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{typ}))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(code)))
}

func TestSharingZeroLocals(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, locals := range []int{1, 8, 64, 256} {
			data := sharingZeroLocalsModule(wide, locals)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			cm, err := benchCompileModule(m)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("wide=%v locals=%d corpus=%x native=%x code_bytes=%d", wide, locals, sha256.Sum256(data), sha256.Sum256(cm.Code), len(cm.Code))
			if err = cm.Close(); err != nil {
				t.Fatal(err)
			}
			c, err := wago.Compile(nil, data)
			if err != nil {
				t.Fatal(err)
			}
			in, err := wago.Instantiate(c)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			got, err := in.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("wide=%v locals=%d: got=%v err=%v", wide, locals, got, err)
			}
			in.Close()
			c.Close()
		}
	}
}

func BenchmarkSharingZeroLocals(b *testing.B) {
	for _, locals := range []int{1, 8, 64, 256} {
		b.Run(fmt.Sprint(locals), func(b *testing.B) {
			m, err := wasm.DecodeModule(sharingZeroLocalsModule(true, locals))
			if err != nil {
				b.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := benchCompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				if err = cm.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

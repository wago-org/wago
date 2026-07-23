//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func stagedExceptionFuncrefProductModule(payloadType []byte, elemFlags byte, refFunc byte, clearRoot bool) []byte {
	tagType := append([]byte{0x60, 0x01}, payloadType...)
	tagType = append(tagType, 0x00)
	indexedNullableResult := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	indexedNullableExnResult := []byte{0x60, 0x00, 0x02, 0x63, 0x00, 0x64, byte(corewasm.HeapExn)}
	throwBody := []byte{0xd2, refFunc, 0x08, 0x00, 0x0b}
	catchBody := []byte{
		0x02, 0x03,
		0x1f, 0x40, 0x01, byte(corewasm.CatchRef), 0x00, 0x00,
		0x10, 0x01,
		0x0b,
		0x00,
		0x0b,
	}
	if clearRoot {
		catchBody = append(catchBody, 0x1a)
	}
	catchBody = append(catchBody, 0x0b)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			tagType,
			indexedNullableResult,
			indexedNullableExnResult,
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00}, []byte{0x00}, []byte{0x02})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("catch", 0, 2))),
	}
	if elemFlags != 0xff {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{elemFlags, 0x00, 0x01, 0x00})))
	}
	sections = append(sections,
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code(throwBody),
			wasmtest.Code(catchBody),
		)),
	)
	return wasmtest.Module(sections...)
}

func compileStagedExceptionFuncrefProduct(t testing.TB, data []byte) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	features.ExceptionReferences = true
	features.TypedFunctionReferences = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if err != nil {
		t.Fatalf("compile staged funcref EH product: %v", err)
	}
	return c
}

func TestStagedExceptionFuncrefProductIdentityLifetimeAndGates(t *testing.T) {
	data := stagedExceptionFuncrefProductModule([]byte{0x64, 0x00}, 0x03, 0x00, true)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("public compile = %v, want a closed public Core 3 feature gate", err)
	}
	c := compileStagedExceptionFuncrefProduct(t, data)
	meta := (&Module{c: c}).Metadata()
	if len(meta.Tags) != 1 || len(meta.Types) == 0 {
		c.Close()
		t.Fatalf("exact funcref-payload metadata = %#v", meta)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		loaded.Close()
		c.Close()
		t.Fatalf("public codec load = %v, want typed/EH feature rejection", err)
	}

	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		c.Close()
		t.Fatal(err)
	}
	got, err := in.Invoke("catch")
	if err != nil || len(got) != 1 || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[0]).FuncRef(), 0) {
		in.Close()
		c.Close()
		t.Fatalf("caught funcref = %v, err=%v; want exact function 0", got, err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		got, err := in.Invoke("catch")
		if err != nil || len(got) != 1 || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[0]).FuncRef(), 0) {
			panic("repeated funcref EH catch failed")
		}
	})
	if allocs != 0 {
		in.Close()
		c.Close()
		t.Fatalf("repeated funcref EH catch allocations = %v, want 0", allocs)
	}
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "exception") {
		in.Close()
		c.Close()
		t.Fatalf("live funcref EH snapshot = %v, want explicit rejection", err)
	}
	if err := in.Close(); err != nil {
		c.Close()
		t.Fatal(err)
	}
	in.lifeMu.Lock()
	released := in.resourcesClosed
	in.lifeMu.Unlock()
	if !released {
		c.Close()
		t.Fatal("local funcref EH instance retained resources after close")
	}
	c.Close()
}

func BenchmarkStagedExceptionFuncrefCatch(b *testing.B) {
	c := compileStagedExceptionFuncrefProduct(b, stagedExceptionFuncrefProductModule([]byte{0x64, 0x00}, 0x03, 0x00, true))
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("catch"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStagedExceptionFuncrefProductRejectsWiderShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "nullable payload", data: stagedExceptionFuncrefProductModule([]byte{0x63, 0x00}, 0x03, 0x00, true), want: "local non-null indexed-function"},
		{name: "externref payload", data: stagedExceptionFuncrefProductModule([]byte{0x6f}, 0x03, 0x00, true), want: "local non-null indexed-function"},
		{name: "missing declaration", data: stagedExceptionFuncrefProductModule([]byte{0x64, 0x00}, 0xff, 0x00, true), want: "declarative local function element"},
		{name: "passive declaration", data: stagedExceptionFuncrefProductModule([]byte{0x64, 0x00}, 0x01, 0x00, true), want: "declarative local function element"},
		{name: "foreign descriptor", data: stagedExceptionFuncrefProductModule([]byte{0x64, 0x00}, 0x03, 0x01, true), want: "exact local payload function"},
		{name: "live root escape", data: stagedExceptionFuncrefProductModule([]byte{0x64, 0x00}, 0x03, 0x00, false), want: "drop it immediately"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := corewasm.DecodeModule(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if err := stagedExceptionHandlingShape(m, true, false); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("shape = %v, want %q", err, tc.want)
			}
		})
	}
}

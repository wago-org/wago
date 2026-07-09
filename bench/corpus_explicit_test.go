package wagobench

import (
	"bytes"
	"os"
	"testing"

	cwasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestCorpusExplicitFannkuch(t *testing.T) {
	b, err := os.ReadFile("corpus/fannkuch.wasm")
	if err != nil {
		t.Fatal(err)
	}
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	comp, err := wago.Compile(cfg, b)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := wago.Instantiate(comp, wago.InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	got, err := in.Invoke("run", wago.I32(8))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0] != 22 {
		t.Fatalf("fannkuch=%d, want 22", got[0])
	}
}

func TestExplicitMemoryCopyForward12(t *testing.T) {
	testExplicitMemoryCopyForward12(t, false, 0)
}

func TestExplicitMemoryCopyForward12Dynamic(t *testing.T) {
	testExplicitMemoryCopyForward12(t, true, 0)
}

func TestExplicitMemoryCopyForward12DynamicHigh(t *testing.T) {
	testExplicitMemoryCopyForward12(t, true, 1048432)
}

func testExplicitMemoryCopyForward12(t *testing.T, dynamic bool, base int32) {
	body := append([]byte{0x41}, wasmtest.SLEB32(base)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(base+48)...)
	var params []cwasm.ValType
	var args []uint64
	if dynamic {
		params = []cwasm.ValType{cwasm.I32}
		args = []uint64{wago.I32(12)}
		body = append(body, 0x20, 0x00) // n = param 0
	} else {
		body = append(body, 0x41, 0x0c) // n = 12
	}
	body = append(body,
		0xfc, 0x0a, 0x00, 0x00, // memory.copy 0 0
		0x0b,
	)
	pages := uint32(1)
	if base != 0 {
		pages = 16
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(append([]byte{0x00}, wasmtest.ULEB(pages)...))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy", 0, 0),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	c, err := wago.Compile(cfg, mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := wago.Instantiate(c, wago.InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	mem := in.Memory().Bytes()
	dst := int(base)
	src := int(base + 48)
	for i := 0; i < 12; i++ {
		mem[src+i] = byte(0xa0 + i)
	}
	if _, err := in.Invoke("copy", args...); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if got, want := mem[dst:dst+12], mem[src:src+12]; !bytes.Equal(got, want) {
		t.Fatalf("copy = % x, want % x", got, want)
	}
}

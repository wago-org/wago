//go:build linux && riscv64 && wago_guardpage && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func rv64GuardStoreModule(vector bool) []byte {
	params := []wasm.ValType{wasm.I32, wasm.I64}
	body := []byte{0x20, 0, 0x20, 1, 0x37, 3, 0, 0x0b}
	if vector {
		params = params[:1]
		body = []byte{0x20, 0, 0xfd, 0x0c}
		body = append(body, bytes.Repeat([]byte{0x5a}, 16)...)
		body = append(body, 0xfd, 0x0b, 4, 0, 0x0b)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, nil))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("store", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64GuardedSplitStoreBoundsAndReuse(t *testing.T) {
	for _, vector := range []bool{false, true} {
		name := "scalar"
		if vector {
			name = "vector"
		}
		t.Run(name, func(t *testing.T) {
			source := rv64GuardStoreModule(vector)
			cfg := compatibilityDefaultConfig().WithBoundsChecks(BoundsChecksSignalsBased)
			c, err := Compile(cfg, source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			data := in.Memory().UnsafeBytes()
			copy(data[len(data)-16:], bytes.Repeat([]byte{0xa5}, 16))
			args := []uint64{65532, 0xdeadbeef01234567}
			size := 8
			if vector {
				args = args[:1]
				size = 16
			}
			_, err = in.Invoke("store", args...)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
				t.Fatal(err)
			}
			if !bytes.Equal(data[len(data)-16:], bytes.Repeat([]byte{0xa5}, 16)) {
				t.Fatal("trap changed memory tail")
			}
			args[0] = 17
			_, err = in.Invoke("store", args...)
			if err != nil {
				t.Fatal("reuse after bounds trap", err)
			}
			if vector && !bytes.Equal(data[17:17+size], bytes.Repeat([]byte{0x5a}, size)) {
				t.Fatal("unaligned vector store")
			}
			_, err = in.Invoke("store", append([]uint64{0xffffffff}, args[1:]...)...)
			if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
				t.Fatal("full address", err)
			}
		})
	}
}

func TestRV64GuardBoundsCodeSize(t *testing.T) {
	source := rv64GuardStoreModule(false)
	var sizes [2]int
	for i, mode := range []BoundsCheckMode{BoundsChecksExplicit, BoundsChecksSignalsBased} {
		c, err := Compile(compatibilityDefaultConfig().WithBoundsChecks(mode), source)
		if err != nil {
			t.Fatal(err)
		}
		sizes[i] = len(c.code)
		c.Close()
	}
	t.Logf("scalar store image bytes explicit=%d guard=%d", sizes[0], sizes[1])
	if sizes[1] >= sizes[0] {
		t.Fatal("guard lowering did not reduce image", sizes)
	}
}

func TestRV64GuardedTrapAfterHostResumeAndReuse(t *testing.T) {
	imp := append(wasmtest.Name("env"), wasmtest.Name("pause")...)
	imp = append(imp, 0, 0)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("store", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x20, 0, 0x42, 7, 0x37, 3, 0, 0x0b}))))
	c, err := Compile(compatibilityDefaultConfig().WithBoundsChecks(BoundsChecksSignalsBased), source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	calls := 0
	pause := slotHostFunc(func(HostModule, []uint64, []uint64) { calls++ })
	in, err := Instantiate(c, testImports("env.pause", pause))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	data := in.Memory().UnsafeBytes()
	copy(data[len(data)-16:], bytes.Repeat([]byte{0xa5}, 16))
	_, err = in.Invoke("store", uint64(65532))
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds || calls != 1 {
		t.Fatal("trap after host resume", err, calls)
	}
	if !bytes.Equal(data[len(data)-16:], bytes.Repeat([]byte{0xa5}, 16)) {
		t.Fatal("resumed trap changed memory")
	}
	_, err = in.Invoke("store", uint64(17))
	if err != nil || calls != 2 || data[17] != 7 {
		t.Fatal("reuse after resumed trap", err, calls, data[17])
	}
}

func TestRV64GuardedForeignTrapOwnerAndReuse(t *testing.T) {
	cfg := compatibilityDefaultConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	calleeCode, err := Compile(cfg, rv64GuardStoreModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer calleeCode.Close()
	callee, err := Instantiate(calleeCode, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer callee.Close()
	target, err := callee.ExportedFunc("store")
	if err != nil {
		t.Fatal(err)
	}
	imp := append(wasmtest.Name("env"), wasmtest.Name("target")...)
	imp = append(imp, 0, 0)
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, nil))), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("relay", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 0, 0x0b}))))
	c, err := Compile(cfg, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	caller, err := Instantiate(c, NewImports().Function("env", "target", target))
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	data := callee.Memory().UnsafeBytes()
	copy(data[len(data)-16:], bytes.Repeat([]byte{0xa5}, 16))
	_, err = caller.Invoke("relay", uint64(65532), uint64(0x1122334455667788))
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
		t.Fatal("foreign guard trap", err)
	}
	if !bytes.Equal(data[len(data)-16:], bytes.Repeat([]byte{0xa5}, 16)) {
		t.Fatal("foreign trap changed tail")
	}
	_, err = caller.Invoke("relay", uint64(17), uint64(7))
	if err != nil || data[17] != 7 {
		t.Fatal("foreign reuse", err, data[17])
	}
	_, err = callee.Invoke("store", uint64(25), uint64(9))
	if err != nil || data[25] != 9 {
		t.Fatal("callee reuse", err, data[25])
	}
}

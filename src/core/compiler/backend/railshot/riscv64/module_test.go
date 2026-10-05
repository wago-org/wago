//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	native "github.com/wago-org/wago/src/core/runtime/riscv64"
)

type fixtureFunc struct {
	params     []byte
	result     byte
	results    []byte
	locals     []byte
	body       []byte
	unexported bool
}

func uleb(b []byte, n uint32) []byte {
	for {
		v := byte(n & 127)
		n >>= 7
		if n != 0 {
			v |= 128
		}
		b = append(b, v)
		if n == 0 {
			return b
		}
	}
}
func sleb(b []byte, n int64) []byte {
	for {
		v := byte(n & 127)
		n >>= 7
		done := n == 0 && v&64 == 0 || n == -1 && v&64 != 0
		if !done {
			v |= 128
		}
		b = append(b, v)
		if done {
			return b
		}
	}
}
func section(b []byte, id byte, p []byte) []byte {
	b = append(b, id)
	b = uleb(b, uint32(len(p)))
	return append(b, p...)
}
func binaryModule(fs ...fixtureFunc) []byte {
	b := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	nexports := uint32(0)
	for _, f := range fs {
		if !f.unexported {
			nexports++
		}
	}
	types, functions, exports, code := uleb(nil, uint32(len(fs))), uleb(nil, uint32(len(fs))), uleb(nil, nexports), uleb(nil, uint32(len(fs)))
	for i, f := range fs {
		types = append(types, 0x60)
		types = uleb(types, uint32(len(f.params)))
		types = append(types, f.params...)
		if f.results != nil {
			types = uleb(types, uint32(len(f.results)))
			types = append(types, f.results...)
		} else if f.result == 0 {
			types = append(types, 0)
		} else {
			types = append(types, 1, f.result)
		}
		functions = uleb(functions, uint32(i))
		if !f.unexported {
			name := []byte{byte('a' + i)}
			exports = uleb(exports, uint32(len(name)))
			exports = append(exports, name...)
			exports = append(exports, 0)
			exports = uleb(exports, uint32(i))
		}
		locals := f.locals
		if locals == nil {
			locals = []byte{0}
		}
		body := append(append([]byte(nil), locals...), f.body...)
		code = uleb(code, uint32(len(body)))
		code = append(code, body...)
	}
	b = section(b, 1, types)
	b = section(b, 3, functions)
	b = section(b, 7, exports)
	return section(b, 10, code)
}
func numericFixture(w bool, op byte, branch bool) fixtureFunc {
	typ := byte(0x7f)
	if w {
		typ = 0x7e
	}
	result := typ
	if op >= 0x46 && op <= 0x5a {
		result = 0x7f
	}
	body := []byte{0x20, 0, 0x20, 1, op}
	if branch {
		body = append(body, 0x04, 0x7f, 0x41, 11, 0x05, 0x41, 22, 0x0b)
		result = 0x7f
	}
	return fixtureFunc{params: []byte{typ, typ}, result: result, locals: []byte{1, 1, typ}, body: append(body, 0x0b)}
}
func TestCompileAdmissionAndImmutableMetadata(t *testing.T) {
	data := binaryModule(numericFixture(false, 0x6a, false), numericFixture(true, 0x7c, false))
	c, err := Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.functions) != 2 || c.functions[0].FrameBytes < 48 || c.functions[1].Entry == 0 {
		t.Fatal(c.functions)
	}
	before := append([]byte(nil), c.code...)
	clear(data)
	infos := c.Functions()
	infos[0].Entry = 123
	if !bytes.Equal(before, c.code) || c.functions[0].Entry != 0 {
		t.Fatal("compiled ownership is mutable")
	}
	if runtime.GOARCH != "riscv64" {
		if i, err := c.Instantiate(native.MinNativeStackBytes); i != nil || !errors.Is(err, native.ErrUnavailable) {
			t.Fatal(i, err)
		}
	}
}
func TestRejectCompleteModule(t *testing.T) {
	good := fixtureFunc{result: 0x7f, body: []byte{0x41, 1, 0x0b}}
	for name, bad := range map[string]fixtureFunc{
		"hidden-fp":          {result: 0x7d, body: []byte{0x43, 0, 0, 0, 0, 0x0b}},
		"hidden-return-call": {result: 0x7f, body: []byte{0x12, 0, 0x0b}},
	} {
		t.Run(name, func(t *testing.T) {
			bad.unexported = true
			data := binaryModule(good, bad)
			m, err := wasm.DecodeModule(data)
			if err != nil || len(m.Exports) != 1 || len(m.Code) != 2 {
				t.Fatal("hidden fixture shape", err)
			}
			if c, err := Compile(data); c != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal(c, err)
			}
		})
	}
	base := binaryModule(good)
	for _, name := range []string{"target_features", "metadata.code.branch_hint", "unknown"} {
		payload := uleb(nil, uint32(len(name)))
		payload = append(payload, name...)
		if name == "metadata.code.branch_hint" {
			payload = append(payload, 0)
		}
		data := append(section(append([]byte(nil), base[:8]...), 0, payload), base[8:]...)
		if c, err := Compile(data); c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatalf("custom %s %v", name, err)
		}
	}
	for _, id := range []byte{4, 9, 13} {
		// Wire policy rejects unsupported sections even when semantically empty.
		if err := admitWire(section(append([]byte(nil), base...), id, []byte{0})); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("section %d: %v", id, err)
		}
	}
	for _, name := range []string{"name", "producers"} {
		payload := uleb(nil, uint32(len(name)))
		payload = append(payload, name...)
		if _, err := Compile(section(append([]byte(nil), base...), 0, payload)); err != nil {
			t.Fatalf("inert %s: %v", name, err)
		}
	}
	// Explicit singleton rec group has the same decoded shape, but wire policy
	// must still reject the unsupported proposal syntax.
	raw := section([]byte{0, 97, 115, 109, 1, 0, 0, 0}, 1, []byte{1, 0x4e, 1, 0x60, 0, 1, 0x7f})
	if err := admitWire(raw); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := Compile([]byte{0, 97, 115}); err == nil {
		t.Fatal("invalid binary accepted")
	}
	if _, err := Compile(make([]byte, MaxModuleBytes+1)); err == nil {
		t.Fatal("unbounded input")
	}
	invalid := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x42, 1, 0x0b}})
	if _, err := Compile(invalid); err == nil || errors.Is(err, ErrUnsupported) {
		t.Fatal("normal validation bypassed", err)
	}
}

func modifySection(data []byte, target byte, id byte, payload []byte, replace bool) []byte {
	r := wasm.ReaderFrom(data[8:])
	for r.HasNext() {
		start := r.Offset()
		sid, err := r.Byte()
		if err != nil {
			panic(err)
		}
		n, err := r.U32()
		if err != nil {
			panic(err)
		}
		end := r.Offset() + int(n)
		if sid == target {
			tail := start
			if replace {
				tail = end
			}
			out := section(append([]byte(nil), data[:8+start]...), id, payload)
			return append(out, data[8+tail:]...)
		}
		if err = r.JumpTo(end); err != nil {
			panic(err)
		}
	}
	if replace {
		panic("missing fixture section")
	}
	return section(append([]byte(nil), data...), id, payload)
}
func TestValidatedUnsupportedSectionsAndUnusedSignatures(t *testing.T) {
	base := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 0, 0x0b}})
	for _, tc := range []struct {
		name       string
		before, id byte
		payload    []byte
	}{
		{"import-global", 3, 2, []byte{1, 1, 'm', 1, 'x', 3, 0x7f, 0}},
		{"table", 7, 4, []byte{1, 0x70, 0, 0}},
		{"memory-quota", 7, 5, []byte{1, 0, 0x81, 2}},
		{"fp-global", 7, 6, []byte{1, 0x7d, 0, 0x43, 0, 0, 0, 0, 0x0b}},
		{"empty-tags", 7, 13, []byte{0}},
		{"passive-element", 10, 9, []byte{1, 1, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := modifySection(base, tc.before, tc.id, tc.payload, false)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal("fixture decode", err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal("fixture validate", err)
			}
			if c, err := Compile(data); c != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal(c, err)
			}
		})
	}
	unused := modifySection(base, 1, 1, []byte{2, 0x60, 0, 1, 0x7f, 0x60, 0, 1, 0x7d}, true)
	if c, err := Compile(unused); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("unused noninteger signature", c, err)
	}
	void := modifySection(binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x0b}}), 1, 1, []byte{1, 0x60, 0, 0}, true)
	start := modifySection(void, 10, 8, []byte{0}, false)
	m, err := wasm.DecodeModule(start)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if c, err := Compile(start); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("start", c, err)
	}
}

func TestNativeInstancesConcurrentReuseAndClose(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	c, err := Compile(binaryModule(numericFixture(false, 0x6a, false)))
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 8; round++ {
		i, err := c.Instantiate(native.MinNativeStackBytes)
		if err != nil {
			t.Fatal(err)
		}
		//lint:ignore SA1012 Deliberately verify rejection of a nil invocation context.
		if _, err = i.Invoke(nil, "a", 1, 2); err == nil {
			t.Fatal("nil context accepted")
		}
		if got, err := i.Invoke(context.TODO(), "a", 1, 2); err != nil || got != 3 {
			t.Fatal(got, err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for n := 0; n < 4; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for k := 0; k < 128; k++ {
					got, err := i.Invoke(context.Background(), "a", 0xdeadbeefffffffff, 2)
					if errors.Is(err, native.ErrClosed) {
						return
					}
					if err != nil || got != 1 {
						t.Errorf("concurrent invoke %x %v", got, err)
						return
					}
					runtime.Gosched()
				}
			}()
		}
		close(start)
		runtime.Gosched()
		if err = i.Close(); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if err = i.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func pressureFixture() fixtureFunc {
	// 256 locals, 48 deferred live values plus local overwrites force real spills
	// even with the default pool, then reloads reach offsets beyond signed12.
	f := fixtureFunc{params: []byte{0x7f}, result: 0x7f, locals: uleb([]byte{1}, 255)}
	f.locals = append(f.locals, 0x7f)
	for i := uint32(1); i <= 48; i++ {
		f.body = append(f.body, 0x20, 0, 0x41)
		f.body = sleb(f.body, int64(i))
		f.body = append(f.body, 0x6a, 0x22)
		f.body = uleb(f.body, i)
	}
	for i := 0; i < 47; i++ {
		f.body = append(f.body, 0x6a)
	}
	f.body = append(f.body, 0x0b)
	return f
}
func joinFixture() fixtureFunc {
	// A live stack prefix and old local version survive nested branch agreements.
	return fixtureFunc{params: []byte{0x7f, 0x7f}, result: 0x7f, locals: []byte{1, 254, 1, 0x7f}, body: []byte{
		0x20, 0, 0x20, 1, 0x04, 0x7f,
		0x20, 0, 0x41, 5, 0x6a, 0x22, 0,
		0x20, 1, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x6a,
		0x05, 0x20, 0, 0x41, 3, 0x6b, 0x22, 0, 0x0b,
		0x6a, 0x20, 0, 0x6a, 0x0b}}
}
func TestSharedFramesSpillsJoinsAndWorkerReuse(t *testing.T) {
	for _, regs := range [][]uint8{scalarRegisters[:], {5, 6, 7}} {
		data := binaryModule(pressureFixture(), joinFixture(), numericFixture(true, 0x7c, false))
		c, err := compile(data, regs)
		if err != nil {
			t.Fatal(err)
		}
		p, j := c.functions[0], c.functions[1]
		t.Logf("registers=%d pressure=%+v join=%+v", len(regs), p, j)
		if p.Spills == 0 || p.Reloads == 0 || p.SpillSlots == 0 || p.FrameBytes <= 2047 || j.SpillSlots == 0 || j.FrameBytes <= 2047 {
			t.Fatalf("did not exercise framed pressure/join: %v %v", p, j)
		}
		for n := 0; n < 8; n++ {
			other, err := compile(data, regs)
			if err != nil || !bytes.Equal(c.code, other.code) {
				t.Fatal("worker reuse changed code", err)
			}
		}
	}
}

func TestNativeScalarModules(t *testing.T) {
	if runtime.GOARCH != "riscv64" {
		t.Skip("RV64 execution only")
	}
	for _, regs := range [][]uint8{scalarRegisters[:], {5, 6, 7}} {
		c, err := compile(binaryModule(pressureFixture(), joinFixture(), numericFixture(true, 0x7c, false)), regs)
		if err != nil {
			t.Fatal(err)
		}
		i, err := c.Instantiate(native.MinNativeStackBytes)
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 16; n++ {
			for _, x := range []uint64{0, 1, 0xffffffff, 0xdeadbeef80000000} {
				want := uint64(uint32(uint32(x)*48 + 1176))
				got, err := i.Invoke(context.Background(), "a", x)
				if err != nil || got != want {
					t.Fatalf("pressure x=%x got=%x want=%x err=%v", x, got, want, err)
				}
				for _, cond := range []uint64{0, 1, 0xdeadbeef00000000, 0xdeadbeef00000001} {
					want := uint32(x)*3 - 6
					if uint32(cond) != 0 {
						want = uint32(x)*3 + 17
					}
					got, err := i.Invoke(context.Background(), "b", x, cond)
					if err != nil || got != uint64(want) {
						t.Fatalf("join x=%x cond=%x got=%x want=%x err=%v", x, cond, got, want, err)
					}
				}
			}
			if got, err := i.Invoke(context.Background(), "c", ^uint64(0), 2); err != nil || got != 1 {
				t.Fatal(got, err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := i.Invoke(ctx, "a", 1); !errors.Is(err, ErrCancelable) {
			t.Fatal(err)
		}
		cancel()
		for _, call := range []struct {
			name string
			args []uint64
		}{{"a", nil}, {"missing", nil}} {
			if _, err := i.Invoke(context.Background(), call.name, call.args...); err == nil {
				t.Fatal("bad call accepted")
			}
		}
		if err = i.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err = i.Invoke(context.Background(), "a", 1); !errors.Is(err, native.ErrClosed) {
			t.Fatal(err)
		}
		if err = i.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShapeChecksEveryFieldAndUnusedType(t *testing.T) {
	m, err := wasm.DecodeModule(binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 0, 0x0b}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*wasm.Module){
		func(m *wasm.Module) { m.UsesCompactImports = true }, func(m *wasm.Module) { m.StringRefs = [][]byte{{1}} }, func(m *wasm.Module) { z := wasm.FuncIdx(0); m.Start = &z },
		func(m *wasm.Module) {
			m.Types = append(append([]wasm.RecType(nil), m.Types...), wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.F32}}}}})
		},
	} {
		copy := *m
		mutate(&copy)
		if err := admitShape(&copy); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
	}
}

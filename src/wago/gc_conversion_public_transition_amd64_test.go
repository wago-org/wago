//go:build linux && amd64 && wago_test_conversion_hooks && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"os/exec"
	"testing"
	"time"
)

func publicSetterConverterBytes() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("t")...), 0x01, 0x6f, 0x00, 0x01))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, // i32.const 0
			0x41, 0x2a, // i32.const 42
			0xfb, 0x1c, // ref.i31
			0xfb, 0x1b, // extern.convert_any
			0x26, 0x00, // table.set 0
			0x0b,
		}))),
	)
}
func TestPublicSetterFalseLoadConcurrentEnablement(t *testing.T) {
	const key = "WAGO_PUBLIC_SETTER_TRANSITION_PROBE"
	if os.Getenv(key) != t.Name() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
		cmd.Env = append(os.Environ(), key+"="+t.Name())
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("public setter transition watchdog: %v\n%s", ctx.Err(), output)
		}
		if err != nil {
			t.Fatalf("public setter transition: %v\n%s", err, output)
		}
		return
	}

	for _, globalMode := range []bool{false, true} {
		t.Run(fmt.Sprint(globalMode), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			c, e := rt.Compile(watToWasm(t, `(module (table (export "t") 1 externref) (global (export "g") (mut externref) (ref.null extern)) (func (export "set") (param externref) (table.set (i32.const 0) (local.get 0))))`))
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			in, e := rt.Instantiate(context.Background(), c)
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			table, e := in.ExportedTable("t")
			if e != nil {
				t.Fatal(e)
			}
			global, e := in.ExportedGlobalObject("g")
			if e != nil {
				t.Fatal(e)
			}
			converter := publicSetterConverterBytes()
			if globalMode {
				converter = wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, nil))),
					wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("g")...), 3, 0x6f, 1))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0xfb, 0x1c, 0xfb, 0x1b, 0x24, 0, 0x0b}))))
			}

			cc, e := rt.Compile(converter)
			if e != nil {
				t.Fatal(e)
			}
			defer cc.Close()
			entered, release, enabled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			conversionPublicFalseHook = func(active *Instance) {
				if active == in {
					close(entered)
					<-release
				}
			}
			conversionPublicEnableHook = func() { close(enabled) }
			defer func() { conversionPublicFalseHook = nil; conversionPublicEnableHook = nil }()
			ref := issueExternref(t, rt, "ordinary")
			callDone := make(chan error, 1)
			go func() {
				if globalMode {
					callDone <- global.SetValue(ValueExternRef(ref))
				} else {
					callDone <- table.SetValue(0, ValueExternRef(ref))
				}
			}()
			select {
			case <-entered:
			case <-time.After(15 * time.Second):
				t.Fatal("false route not reached")
			}
			if nativeExecutionMu.TryLock() {
				nativeExecutionMu.Unlock()
				close(release)
				<-callDone
				t.Fatal("false publication lacks shared native lock")
			}
			type result struct {
				in *Instance
				e  error
			}
			built := make(chan result, 1)
			go func() {
				imports := NewImports().Table("env", "t", table)
				if globalMode {
					imports = NewImports().Global("env", "g", global)
				}
				v, e := rt.Instantiate(context.Background(), cc, WithImports(imports))
				built <- result{v, e}
			}()
			select {
			case <-enabled:
			case <-time.After(15 * time.Second):
				close(release)
				<-callDone
				t.Fatal("enabling constructor did not reach epoch publication")
			}
			if !rt.refStore.conversionDomainEverEnabled.Load() {
				t.Fatal("epoch not published")
			}
			// No timing oracle: a successful TryLock would contradict shared lease ownership.
			if nativeExecutionMu.TryLock() {
				nativeExecutionMu.Unlock()
				t.Error("shared mutation lock lost while false write paused")
			}
			close(release)
			if e = <-callDone; e != nil {
				t.Fatal(e)
			}
			var conv *Instance
			select {
			case r := <-built:
				if r.e != nil {
					t.Fatal(r.e)
				}
				conv = r.in
			case <-time.After(15 * time.Second):
				t.Fatal("constructor blocked after false write completed")
			}
			conversionPublicFalseHook = nil
			conversionPublicEnableHook = nil
			if globalMode {
				if binary.LittleEndian.Uint64(global.cell) != ref.token {
					t.Fatal("false global write lost")
				}
			} else if binary.LittleEndian.Uint64(table.desc[8:]) != ref.token {
				t.Fatal("false table write lost")
			}
			if _, e = conv.Invoke("run"); e != nil {
				t.Fatal(e)
			}
			desc := table.desc
			if globalMode {
				desc = global.cell
			}
			h2WithAuthority(t, rt.refStore, func() {
				c := rt.refStore.gcDomains.conversionContainers[&desc[0]]
				if c == nil || c.slots[0] == nil {
					t.Fatal("later conversion receipt absent")
				}
			})
			word := binary.LittleEndian.Uint64(desc)
			if !globalMode {
				word = binary.LittleEndian.Uint64(desc[8:])
			}
			public, ok := conversionBoundaryWord(in, word, false)
			if !ok || public == 0 {
				t.Fatal("actual outgoing boundary failed")
			}
			internal, ok := conversionBoundaryWord(in, public, true)
			if !ok || internal != word {
				t.Fatal("actual incoming boundary failed")
			}
			if globalMode {
				e = global.SetValue(ValueOf(ValExternRef, public))
			} else {
				e = table.SetValue(0, ValueOf(ValExternRef, public))
			}
			if e != nil {
				t.Fatal(e)
			}
			if globalMode {
				e = global.SetValue(ValueExternRef(ref))
			} else {
				e = table.SetValue(0, ValueExternRef(ref))
			}
			if e != nil {
				t.Fatal(e)
			}
			h2WithAuthority(t, rt.refStore, func() {
				if rt.refStore.gcDomains.conversionContainers[&desc[0]].slots[0] != nil {
					t.Fatal("ordinary overwrite left receipt")
				}
			})
			if e = conv.Close(); e != nil {
				t.Fatal(e)
			}
			if !rt.refStore.conversionDomainEverEnabled.Load() {
				t.Fatal("close reset epoch")
			}
			t.Log("false write held shared native authority across concurrent first true publication; later receipt and ordinary overwrite validated")

		})
	}
}

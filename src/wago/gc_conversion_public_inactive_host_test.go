//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestConversionBoundaryInactiveStoreAvoidsDirectoryLock(t *testing.T) {
	const child = "WAGO_CONVERSION_BOUNDARY_LOCK_PROBE"
	if os.Getenv(child) != t.Name() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
		cmd.Env = append(os.Environ(), child+"="+t.Name())
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("inactive boundary probe did not complete before watchdog: %v\n%s", ctx.Err(), output)
		}
		if err != nil {
			t.Fatalf("inactive boundary probe: %v\n%s", err, output)
		}
		return
	}

	rt := NewRuntime()
	defer rt.Close()
	compiled, err := rt.Compile(watToWasm(t, `(module
		(func (export "identity") (param externref) (result externref) (local.get 0))
	)`))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	word := issueExternref(t, rt, "non-null boundary").token
	if in.refStore.conversionDomainEverEnabled.Load() {
		t.Fatal("ordinary Store unexpectedly enabled conversion")
	}

	// This isolates conversionBoundaryWord. Public invocation retains separate
	// token-authentication and admission uses of store.mu by design.
	in.refStore.mu.Lock()
	for _, incoming := range []bool{false, true} {
		if got, converted := conversionBoundaryWord(in, word, incoming); converted || got != 0 {
			in.refStore.mu.Unlock()
			t.Fatalf("inactive boundary direction %v converted %#x", incoming, got)
		}
	}
	in.refStore.mu.Unlock()
}

func TestInactivePublicReferenceSettersBypassConversionWorkAfterValidation(t *testing.T) {
	rt, foreignRuntime := NewRuntime(), NewRuntime()
	defer rt.Close()
	defer foreignRuntime.Close()
	compiled, err := rt.Compile(watToWasm(t, `(module
		(table (export "table") 1 1 externref)
		(global (export "global") (mut externref) (ref.null extern))
	)`))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	table, err := in.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	global, err := in.ExportedGlobalObject("global")
	if err != nil {
		t.Fatal(err)
	}
	ordinary := issueExternref(t, rt, "ordinary public value")
	foreign := issueExternref(t, foreignRuntime, "foreign public value")
	if in.refStore.conversionDomainEverEnabled.Load() {
		t.Fatal("ordinary Store unexpectedly enabled conversion")
	}

	// Preserve an already-existing empty container: false-state publication must
	// neither require it nor create or mutate conversion receipt state.
	container, err := newGCConversionContainer(in, table.desc)
	if err != nil {
		t.Fatal(err)
	}
	if len(container.slots) != 0 {
		t.Fatal("new ordinary container unexpectedly has receipts")
	}
	containerCount := func() int {
		in.refStore.mu.Lock()
		defer in.refStore.mu.Unlock()
		if in.refStore.gcDomains == nil {
			return 0
		}
		return len(in.refStore.gcDomains.conversionContainers)
	}
	before := containerCount()

	if err = table.SetValue(0, ValueExternRef(ordinary)); err != nil {
		t.Fatal(err)
	}
	if err = global.SetValue(ValueExternRef(ordinary)); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(table.desc[8:]); got != ordinary.token {
		t.Fatalf("table word = %#x, want %#x", got, ordinary.token)
	}
	if got := binary.LittleEndian.Uint64(global.cell); got != ordinary.token {
		t.Fatalf("global word = %#x, want %#x", got, ordinary.token)
	}
	if after := containerCount(); after != before {
		t.Fatalf("conversion container count = %d, want unchanged %d", after, before)
	}
	if len(container.slots) != 0 {
		t.Fatal("inactive public publication prepared a conversion receipt")
	}

	for name, set := range map[string]func() error{
		"table foreign":  func() error { return table.SetValue(0, ValueExternRef(foreign)) },
		"table forged":   func() error { return table.SetValue(0, ValueOf(ValExternRef, ordinary.token^0x8000000000000000)) },
		"table type":     func() error { return table.SetValue(0, ValueI32(1)) },
		"table bounds":   func() error { return table.SetValue(1, ValueExternRef(ordinary)) },
		"global foreign": func() error { return global.SetValue(ValueExternRef(foreign)) },
		"global forged":  func() error { return global.SetValue(ValueOf(ValExternRef, ordinary.token^0x8000000000000000)) },
		"global type":    func() error { return global.SetValue(ValueI32(1)) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := set(); err == nil {
				t.Fatal("invalid publication accepted")
			}
			if got := binary.LittleEndian.Uint64(table.desc[8:]); got != ordinary.token {
				t.Fatalf("rejection changed table to %#x", got)
			}
			if got := binary.LittleEndian.Uint64(global.cell); got != ordinary.token {
				t.Fatalf("rejection changed global to %#x", got)
			}
			if after := containerCount(); after != before || len(container.slots) != 0 {
				t.Fatalf("rejection entered conversion work: containers %d/%d, receipts %d", after, before, len(container.slots))
			}
		})
	}
}

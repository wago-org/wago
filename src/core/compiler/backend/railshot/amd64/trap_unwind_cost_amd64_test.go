//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestFunctionLocalTrapUnwindSharingIsNotProfitableAMD64(t *testing.T) {
	a := &amd64.Asm{}
	start := a.Len()
	a.Load64(RSP, RBX, -offTrapStackReentry)
	a.Ret()
	unwindBytes := a.Len() - start

	b := &amd64.Asm{}
	b.JmpPlaceholder()
	jumpBytes := b.Len()
	if unwindBytes != 5 || jumpBytes != 5 {
		t.Fatalf("trap unwind/jump bytes = %d/%d, want 5/5", unwindBytes, jumpBytes)
	}
	// N local tails cost N*5 bytes; N jumps plus one shared five-byte tail
	// cost N*5+5. Sharing can never cross over without a shorter transfer.
	for groups := 2; groups <= 32; groups++ {
		local := groups * unwindBytes
		shared := groups*jumpBytes + unwindBytes
		if shared <= local {
			t.Fatalf("%d groups unexpectedly cross over: local=%d shared=%d", groups, local, shared)
		}
	}
}

func TestSizeSharesTrapBodiesAcrossFunctionsAMD64(t *testing.T) {
	oldInline := inlineEnabled
	inlineEnabled = false
	t.Cleanup(func() { inlineEnabled = oldInline })
	i32 := []wasm.ValType{wasm.I32}
	callee := []byte{
		0x00,
		0x20, 0x00, 0x45,
		0x04, 0x7f,
		0x00,
		0x05,
		0x20, 0x00, 0x41, 0x01, 0x46,
		0x04, 0x7f,
		0x41, 0x01, 0x41, 0x00, 0x6e,
		0x05,
		0x41, 0x80, 0x80, 0x80, 0x80, 0x78, 0x41, 0x7f, 0x6d,
		0x0b,
		0x0b,
		0x0b,
	}
	m := modFuncs(t,
		funcDef{i32, i32, []byte{
			0x00,
			0x20, 0x00,
			0x04, 0x7f,
			0x20, 0x00, 0x10, 0x01,
			0x05,
			0x20, 0x00, 0x10, 0x02,
			0x0b,
			0x0b,
		}},
		funcDef{i32, i32, callee},
		funcDef{i32, i32, callee},
	)
	for _, workers := range []int{1, 2} {
		var stats ModuleStats
		opts := CompileOptions{CompactNative: true, Stats: &stats, Workers: workers}
		if _, err := CompileModuleWith(m, opts); err != nil {
			t.Fatal(err)
		}
		if got := stats.Funcs[1].Peephole["module-shared-trap-body"] + stats.Funcs[2].Peephole["module-shared-trap-body"]; got != 1 {
			t.Fatalf("workers=%d module trap shares = %d, want 1", workers, got)
		}
		if stats.NativeSize.AccountedBytes() != stats.NativeSize.TotalBytes {
			t.Fatalf("workers=%d native ledger = %+v", workers, stats.NativeSize)
		}
		for _, arg := range []uint64{0, 1, 2} {
			if _, _, err := runMemAmd64WithOptions(t, m, CompileOptions{CompactNative: true, Workers: workers}, nil, arg); err == nil {
				t.Fatalf("workers=%d argument %d did not trap through shared body", workers, arg)
			}
		}
	}
}

func TestSizeSharesCompleteTrapBodyAMD64(t *testing.T) {
	before := sharedTrapBodyEnabled
	t.Cleanup(func() { sharedTrapBodyEnabled = before })
	emit := func(enabled bool) (int, *CodegenStats) {
		sharedTrapBodyEnabled = enabled
		a := &amd64.Asm{}
		sc := &scratch{}
		stats := &CodegenStats{}
		f := fn{
			a:      a,
			sc:     sc,
			stats:  stats,
			policy: CodegenPolicy{CompactNative: true},
		}
		for code := uint32(1); code <= 3; code++ {
			branch := a.JccPlaceholder(condNE)
			sc.trapSites[code] = append(sc.trapSites[code], trapSite{
				branch: branch, function: 4, pc: code * 10,
			})
		}
		f.emitTrapStubs()
		return a.Len(), stats
	}

	rollbackBytes, _ := emit(false)
	sharedBytes, stats := emit(true)
	if sharedBytes >= rollbackBytes {
		t.Fatalf("shared trap bytes = %d, rollback = %d; want shrink", sharedBytes, rollbackBytes)
	}
	if stats.Peephole["shared-trap-body"] != 1 || stats.TrapStubs != 3 || stats.TrapGroups != 3 {
		t.Fatalf("shared trap stats = stubs:%d groups:%d peep:%v", stats.TrapStubs, stats.TrapGroups, stats.Peephole)
	}
}

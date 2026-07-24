//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWideCrossInstanceIndirectCallScratch proves that home-aware call_indirect
// keeps its manually staged home/context pointers disjoint from wide-stack flush
// staging. The caller leaves 65 scalar slots live below the table index, forcing
// flushWideStack, while the target comes from another instance through an
// imported host table. The target's global distinguishes its instance context
// from the caller's identically indexed global.
func TestWideCrossInstanceIndirectCallScratch(t *testing.T) {
	const (
		belowSum  = int64(65 * 66 / 2)
		targetCtx = int64(100)
		callerCtx = int64(10_000)
	)

	tests := []struct {
		name         string
		typeParams   string
		typeResults  string
		targetBody   string
		callArgs     string
		callerResult string
		callerAfter  string
		wantSlots    []uint64
		wantTarget   int64
	}{
		{
			name:         "zero results",
			typeParams:   `(param v128)`,
			targetBody:   `(global.set $ctx (i64.add (global.get $ctx) (i64.const 1)))`,
			callArgs:     `(v128.const i64x2 7 8)`,
			callerResult: `(result i64)`,
			callerAfter:  wideIndirectAdds(64),
			wantSlots:    []uint64{uint64(belowSum)},
			wantTarget:   targetCtx + 1,
		},
		{
			name:         "one scalar result",
			typeParams:   `(param v128)`,
			typeResults:  `(result i64)`,
			targetBody:   `(global.get $ctx)`,
			callArgs:     `(v128.const i64x2 7 8)`,
			callerResult: `(result i64)`,
			callerAfter:  wideIndirectAdds(65),
			wantSlots:    []uint64{uint64(belowSum + targetCtx)},
			wantTarget:   targetCtx,
		},
		{
			name:         "v128 result layout",
			typeResults:  `(result v128 i64)`,
			targetBody:   `(v128.const i64x2 123 456) (global.get $ctx)`,
			callerResult: `(result i64 v128) (local $vec v128) (local $ctxValue i64)`,
			callerAfter:  `(local.set $ctxValue) (local.set $vec) ` + wideIndirectAdds(64) + ` (local.get $ctxValue) (i64.add) (local.get $vec)`,
			wantSlots:    []uint64{uint64(belowSum + targetCtx), 123, 456},
			wantTarget:   targetCtx,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			table, err := NewTable(1, 1)
			if err != nil {
				t.Fatalf("NewTable: %v", err)
			}
			defer table.Close()

			producerCode := mustCompileWat(rt, t, fmt.Sprintf(`(module
				(type $targetType (func %s %s))
				(import "env" "table" (table 1 1 funcref))
				(global $ctx (export "ctx") (mut i64) (i64.const %d))
				(func $target (type $targetType) %s %s)
				(elem (i32.const 0) func $target))`, tc.typeParams, tc.typeResults, targetCtx, tc.typeParams+" "+tc.typeResults, tc.targetBody))
			producer, err := rt.Instantiate(context.Background(), producerCode, WithImports(Imports{"env.table": table}))
			if err != nil {
				t.Fatalf("instantiate producer: %v", err)
			}
			defer producer.Close()

			callerCode := mustCompileWat(rt, t, fmt.Sprintf(`(module
				(type $targetType (func %s %s))
				(import "env" "table" (table 1 1 funcref))
				(global $ctx (export "ctx") (mut i64) (i64.const %d))
				(func (export "call") %s
					%s
					%s
					(i32.const 0)
					(call_indirect (type $targetType))
					%s))`, tc.typeParams, tc.typeResults, callerCtx, tc.callerResult, wideIndirectConstants(), tc.callArgs, tc.callerAfter))
			caller, err := rt.Instantiate(context.Background(), callerCode, WithImports(Imports{"env.table": table}))
			if err != nil {
				t.Fatalf("instantiate caller: %v", err)
			}
			defer caller.Close()

			got, err := caller.Invoke("call")
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(got) != len(tc.wantSlots) {
				t.Fatalf("call slots = %#v, want %#v", got, tc.wantSlots)
			}
			for i := range got {
				if got[i] != tc.wantSlots[i] {
					t.Fatalf("call slot %d = %#x, want %#x (all below-call values and results must survive)", i, got[i], tc.wantSlots[i])
				}
			}
			if gotCtx := readI64Export(t, producer, "ctx"); gotCtx != tc.wantTarget {
				t.Fatalf("producer context global = %d, want %d", gotCtx, tc.wantTarget)
			}
			if gotCtx := readI64Export(t, caller, "ctx"); gotCtx != callerCtx {
				t.Fatalf("caller context global = %d, want unchanged %d", gotCtx, callerCtx)
			}
		})
	}
}

func wideIndirectConstants() string {
	var b strings.Builder
	for i := 1; i <= 65; i++ {
		fmt.Fprintf(&b, "(i64.const %d) ", i)
	}
	return b.String()
}

func wideIndirectAdds(n int) string {
	return strings.Repeat("(i64.add) ", n)
}

func readI64Export(t *testing.T, in *Instance, name string) int64 {
	t.Helper()
	bits, err := in.Global(name)
	if err != nil {
		t.Fatalf("Global(%q): %v", name, err)
	}
	return int64(bits)
}

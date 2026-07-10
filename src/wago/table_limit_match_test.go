//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"strings"
	"testing"
)

// A Wasm-exported table with no declared maximum receives a finite allocation
// reservation, but its external type is still unbounded. Import limit-matching
// must reject a consumer that requires a finite maximum (an unbounded table
// cannot satisfy a bounded import) and accept one with no maximum requirement.
func TestImportedTableRejectsUnboundedForBoundedImport(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	producerMod, err := rt.Compile(watToWasm(t, `(module (table (export "t") 1 funcref))`))
	if err != nil {
		t.Fatalf("compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer producer.Close()
	table, err := producer.ExportedTable("t")
	if err != nil {
		t.Fatalf("ExportedTable: %v", err)
	}

	boundedMod, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 1 100 funcref)))`))
	if err != nil {
		t.Fatalf("compile bounded consumer: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), boundedMod, WithImports(Imports{"env.t": table})); err == nil || !strings.Contains(err.Error(), "no declared maximum") {
		t.Fatalf("bounded import of unbounded table error = %v, want 'no declared maximum' rejection", err)
	}

	unboundedMod, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 1 funcref)))`))
	if err != nil {
		t.Fatalf("compile unbounded consumer: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), unboundedMod, WithImports(Imports{"env.t": table}))
	if err != nil {
		t.Fatalf("unbounded import of unbounded table should succeed: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer Close: %v", err)
	}
}

// A bounded exported table matches an import whose maximum is >= the declared
// maximum, and is rejected when its declared maximum exceeds the import's.
func TestImportedTableBoundedMaxMatching(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	producerMod, err := rt.Compile(watToWasm(t, `(module (table (export "t") 1 50 funcref))`))
	if err != nil {
		t.Fatalf("compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("instantiate producer: %v", err)
	}
	defer producer.Close()
	table, err := producer.ExportedTable("t")
	if err != nil {
		t.Fatalf("ExportedTable: %v", err)
	}

	okMod, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 1 100 funcref)))`))
	if err != nil {
		t.Fatalf("compile widening consumer: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), okMod, WithImports(Imports{"env.t": table}))
	if err != nil {
		t.Fatalf("declared max 50 within import max 100 should succeed: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer Close: %v", err)
	}

	tightMod, err := rt.Compile(watToWasm(t, `(module (import "env" "t" (table 1 20 funcref)))`))
	if err != nil {
		t.Fatalf("compile tightening consumer: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), tightMod, WithImports(Imports{"env.t": table})); err == nil || !strings.Contains(err.Error(), "required maximum") {
		t.Fatalf("declared max 50 > import max 20 error = %v, want 'required maximum' rejection", err)
	}
}

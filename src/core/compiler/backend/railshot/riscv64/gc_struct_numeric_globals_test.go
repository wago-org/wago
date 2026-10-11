//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCStructNumericGlobalsExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCStructNumericGlobalsHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 67 || sha256.Sum256(source) != rv64GCStructNumericGlobalsSourceDigest || !admitRV64GCStructNumericGlobalsWire(source) || !AdmitGCStructNumericGlobalsSource(source) || !admitRV64GCStructNumericGlobals(m) {
		t.Fatalf("exact numeric struct globals fixture was not admitted: length=%d digest=%x wire=%t decoded=%t", len(source), sha256.Sum256(source), admitRV64GCStructNumericGlobalsWire(source), admitRV64GCStructNumericGlobals(m))
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructGlobalsSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 {
		t.Fatalf("numeric struct globals emitted executable state: code=%d entries=%v/%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCStructGlobalsSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("numeric struct globals compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCStructNumericGlobalsWire(candidate) {
			t.Fatalf("wire mutation %d admitted", i)
		}
	}
}

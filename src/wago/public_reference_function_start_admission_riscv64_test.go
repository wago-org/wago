//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceStartCopy(c *Compiled) *Compiled {
	v := rv64ReferenceImportTailCopy(c)
	v.Entry, v.InternalEntry, v.code = slices.Clone(v.Entry), slices.Clone(v.InternalEntry), slices.Clone(v.code)
	return v
}

func TestRV64PublicReferenceFunctionDefinedStartVoidImportedStart(t *testing.T) {
	c := rv64NativeTailCompile(t, "reference-start-void-import-admission", wasmtest.ClosedReferenceImportedVoidStart(false), false)
	v := cloneCompiledMetadata(c.executionView())
	v.HasStart = true
	v.StartIsImport = true
	if err := rv64ValidateReferenceFunctions(v); err != nil {
		t.Fatal("selected void imported start rejected", err)
	}
}

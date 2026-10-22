//go:build linux && amd64 && !tinygo

package riscv64

import (
	core "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestCurrentCoreStackContract(t *testing.T) {
	if DefaultNativeStackBytes != core.DefaultNativeStackBytes || MinNativeStackBytes != core.MinNativeStackBytes || MaxNativeStackBytes != core.MaxNativeStackBytes || trapBufferBytes != core.TrapBufferBytes {
		t.Fatal("current runtime stack/trap contract drifted")
	}
	e, err := core.NewEngineWithStackBytes(core.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if e.StackTop()-e.StackLimit() != uintptr(core.MinNativeStackBytes-stackFenceMargin) {
		t.Fatal("current stack fence margin drifted")
	}
}

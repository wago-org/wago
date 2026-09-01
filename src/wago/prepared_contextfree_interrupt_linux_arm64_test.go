//go:build linux && arm64 && wago_guardpage && !tinygo

package wago

import (
	"testing"
	"time"

	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCloseInterruptsSignalBackedPreparedContextFreeLoop(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}))),
	)
	compiled, err := Compile(NewRuntimeConfig().
		WithBoundsChecks(BoundsChecksSignalsBased).
		WithCompiler(CompilerDragline).
		WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.PrepareFunction("spin")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.privateFast || !prepared.isolatedFast || !prepared.privateLifetime || !prepared.directTrapIntFast {
		t.Fatalf("signal-backed loop selected private=%t isolated=%t lifetime=%t direct-trap=%t", prepared.privateFast, prepared.isolatedFast, prepared.privateLifetime, prepared.directTrapIntFast)
	}
	done := make(chan error, 1)
	go func() {
		_, err := prepared.Invoke0()
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		requireInterruptedTrap(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not interrupt the prepared context-free loop")
	}
}

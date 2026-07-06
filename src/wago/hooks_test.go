package wago

import (
	"context"
	"fmt"
	"testing"
)

// hookExt is an extension that registers invoke/compile hooks driven by callbacks,
// so a test can observe when they fire.
type hookExt struct {
	afterCompile func(*Module)
	beforeInvoke func(*InvokeContext) error
	afterInvoke  func(*InvokeContext, []Value, error)
}

func (hookExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.hooks", Version: "1.0.0", Stability: Stable}
}

func (e hookExt) Register(reg *Registry) error {
	// Provide env.f so callsEnvF modules instantiate.
	reg.ImportModule("env").
		Func("f", func(_ HostModule, p, r []uint64) { r[0] = p[0] }).
		Params(ValI32).Results(ValI32)
	if e.afterCompile != nil {
		reg.Hooks().AfterCompile(func(_ *CompileContext, m *Module) error { e.afterCompile(m); return nil })
	}
	if e.beforeInvoke != nil {
		reg.Hooks().BeforeInvoke(e.beforeInvoke)
	}
	if e.afterInvoke != nil {
		reg.Hooks().AfterInvoke(e.afterInvoke)
	}
	return nil
}

func TestInvokeHooksFire(t *testing.T) {
	var before, after int
	var sawExport string
	var sawResult int32
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInvoke: func(ic *InvokeContext) error { before++; sawExport = ic.Export; return nil },
		afterInvoke: func(_ *InvokeContext, out []Value, _ error) {
			after++
			if len(out) == 1 {
				sawResult = out[0].I32()
			}
		},
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	if _, err := in.Call(context.Background(), "g", ValueI32(41)); err != nil {
		t.Fatalf("call: %v", err)
	}
	if before != 1 || after != 1 {
		t.Fatalf("hooks fired before=%d after=%d, want 1/1", before, after)
	}
	if sawExport != "g" {
		t.Fatalf("BeforeInvoke saw export %q, want g", sawExport)
	}
	if sawResult != 41 {
		t.Fatalf("AfterInvoke saw result %d, want 41", sawResult)
	}
}

func TestBeforeInvokeVetoAbortsCall(t *testing.T) {
	afterErr := "none"
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInvoke: func(_ *InvokeContext) error { return fmt.Errorf("denied") },
		afterInvoke: func(_ *InvokeContext, out []Value, err error) {
			if err != nil {
				afterErr = err.Error()
			}
		},
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	_, err = in.Call(context.Background(), "g", ValueI32(1))
	if err == nil || err.Error() != "denied" {
		t.Fatalf("Call error = %v, want denied", err)
	}
	if afterErr != "denied" {
		t.Fatalf("AfterInvoke saw err %q, want denied", afterErr)
	}
}

func TestAfterCompileHookFires(t *testing.T) {
	fired := 0
	var gotExports []string
	rt := NewRuntime()
	if err := rt.Use(hookExt{afterCompile: func(m *Module) { fired++; gotExports = m.Exports() }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	_ = callsEnvF(t, rt) // rt.Compile triggers AfterCompile
	if fired != 1 {
		t.Fatalf("AfterCompile fired %d times, want 1", fired)
	}
	if len(gotExports) != 1 || gotExports[0] != "g" {
		t.Fatalf("AfterCompile module exports = %v, want [g]", gotExports)
	}
}

// TestLowLevelInvokeSkipsHooks confirms the low-level Invoke path is hook-free.
func TestLowLevelInvokeSkipsHooks(t *testing.T) {
	rt := NewRuntime()
	fired := 0
	if err := rt.Use(hookExt{beforeInvoke: func(_ *InvokeContext) error { fired++; return nil }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	// Low-level Invoke (untyped) must not fire the runtime's invoke hooks.
	if _, err := in.Invoke("g", I32(3)); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if fired != 0 {
		t.Fatalf("low-level Invoke fired %d invoke hooks, want 0", fired)
	}
}

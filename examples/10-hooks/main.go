// Example 10: lifecycle hooks (observability).
//
// Hooks let a plugin observe compile and invoke without the guest knowing. This
// is how tracing/metrics auto-instrumentation works: BeforeInvoke/AfterInvoke
// wrap every Instance.Call. Run:
//
//	go run ./examples/10-hooks
package main

import (
	"context"
	"fmt"
	"time"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/mods"
)

// tracer is a plugin that times every invocation via invoke hooks.
type tracer struct{}

func (tracer) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{ID: "example.tracer", Version: "1.0.0", Stability: wago.Experimental}
}

func (tracer) Register(reg *wago.Registry) error {
	reg.Hooks().AfterCompile(func(_ *wago.CompileContext, m *wago.Module) error {
		fmt.Printf("[trace] compiled module, exports=%v\n", m.Exports())
		return nil
	})
	reg.Hooks().BeforeInvoke(func(ic *wago.InvokeContext) error {
		ic.Metadata["start"] = time.Now()
		fmt.Printf("[trace] -> %s(%v)\n", ic.Export, ic.Args)
		return nil
	})
	reg.Hooks().AfterInvoke(func(ic *wago.InvokeContext, out []wago.Value, err error) {
		elapsed := time.Since(ic.Metadata["start"].(time.Time))
		fmt.Printf("[trace] <- %s => %v (%s, err=%v)\n", ic.Export, out, elapsed.Round(time.Microsecond), err)
	})
	return nil
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	_ = rt.Use(tracer{})

	mod, _ := rt.Compile(mods.Add())
	ctx := context.Background()
	inst, _ := rt.Instantiate(ctx, mod)
	defer inst.Close()

	// Every Call is wrapped by the hooks above.
	_, _ = inst.Call(ctx, "add", wago.ValueI32(20), wago.ValueI32(22))
}

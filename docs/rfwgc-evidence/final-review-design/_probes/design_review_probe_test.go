//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Design-review probe: a GC producer with a private collector and no externref
// storage exports a scalar function. A consumer that merely declares an
// externref global (or table) imports it into the same Runtime.
func TestDesignReviewExternStorageImportsPrivateGCProducer(t *testing.T) {
	// producer: type0 struct(i32), type1 func()->i32; f allocates and returns 7.
	producers := map[string][]byte{
		"global": wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x24, 0, 0x41, 7, 0x0b}))),
		),
		"alloc": wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x1a, 0x41, 7, 0x0b}))),
		),
		"convert": wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, []byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0xfb, 0x1b, 0x1a, 0x41, 7, 0x0b}))),
		),
	}
	imp := append(append(wasmtest.Name("p"), wasmtest.Name("f")...), 0, 0)
	consumers := map[string][]byte{
		"none": wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(imp)),
			wasmtest.Section(3, wasmtest.Vec([]byte{0})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
		),
		"externref-global": wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(imp)),
			wasmtest.Section(3, wasmtest.Vec([]byte{0})),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
		),
		"externref-table": wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(imp)),
			wasmtest.Section(3, wasmtest.Vec([]byte{0})),
			wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0, 1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
		),
	}
	for _, kind := range []string{"alloc", "convert", "global"} {
		producer := producers[kind]
		for _, shared := range []bool{true, false} {
			for _, name := range []string{"none", "externref-global", "externref-table"} {
				t.Run(fmt.Sprintf("%s/runtime=%t/%s", kind, shared, name), func(t *testing.T) {
					rt := NewRuntime()
					var store *referenceStore
					if shared {
						store = rt.refStore
					}
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
					pc, err := Compile(cfg, producer)
					if err != nil {
						t.Fatal(err)
					}
					defer pc.Close()
					p, err := instantiateCore(pc, InstantiateOptions{store: store})
					if err != nil {
						t.Fatal(err)
					}
					defer p.Close()
					uc, err := Compile(cfg, wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 2, 0x7e, 1, 0x7c, 1}, []byte{0x60, 0, 0})),
						wasmtest.Section(3, wasmtest.Vec([]byte{1})),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x42, 1, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0xfb, 0, 0, 0x1a, 0x0b}))),
					))
					if err != nil {
						t.Fatal(err)
					}
					defer uc.Close()
					unrelated, err := instantiateCore(uc, InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 8192}})
					if err != nil {
						t.Fatal(err)
					}
					defer unrelated.Close()
					private, hasDomain := false, false
					if p.refStore != nil {
						p.refStore.mu.Lock()
						entry := p.refStore.instances[p]
						hasDomain = entry != nil && entry.gcDomain != nil
						private = hasDomain && entry.gcDomain.private
						p.refStore.mu.Unlock()
					}
					t.Logf("producer gc=%t store=%t domain=%t privateDomain=%t", p.gc != nil, p.refStore != nil, hasDomain, private)
					f, err := p.ExportedFunc("f")
					if err != nil {
						t.Fatal(err)
					}
					cc, err := Compile(cfg, consumers[name])
					if err != nil {
						t.Fatal(err)
					}
					defer cc.Close()
					c, err := instantiateCore(cc, InstantiateOptions{store: store, Imports: testImports("p.f", f)})
					if err != nil {
						t.Fatalf("consumer instantiation: %v", err)
					}
					defer c.Close()
					d := c.gcInvocationDomains()
					t.Logf("consumer dynamic=%t imported=%t leasedDomains=%d", c.executionFlags.Load()&executionFlagDynamicGCDomain != 0, c.executionFlags.Load()&executionFlagImportedGCDomain != 0, d.len())
					if shared {
						held := c.lockGCInvocation(newInvocationID())
						ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
						other, lerr := unrelated.lockGCInvocationContext(ctx, newInvocationID())
						cancel()
						if lerr == nil {
							other.unlock()
						}
						held.unlock()
						t.Logf("unrelated lease while consumer lease held: err=%v", lerr)
						if lerr != nil {
							t.Errorf("unrelated GC instance blocked by consumer invocation lease: %v", lerr)
						}
					}
					out, err := c.Invoke("run")
					if err != nil || len(out) != 1 || out[0] != 7 {
						t.Fatalf("run = %v, %v", out, err)
					}
				})
			}
		}
	}
}

//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"slices"
	"testing"
)

func TestRV64PublicReferenceFunctionStateQuota(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			pc := rv64NativeTailCompile(t, "reference-state-quota-producer", wasmtest.ClosedReferenceState(vector, true, true), false)
			cc := rv64NativeTailCompile(t, "reference-state-quota-consumer", rv64ReferenceStateConsumer(vector, "reference-tail"), false)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			export, err := producer.ExportedFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			imports := testImports("env.target", export)
			caller := rv64NativeTokenInstantiate(t, rt, cc, imports)
			// These are real uncached production snapshots under the physical root.
			func() {
				gate := caller.lockInvocation(0)
				defer gate.unlockInvocation()
				entry, err := caller.beginNativeEntry()
				if err != nil {
					t.Fatal(err)
				}
				defer caller.unlockNativeEntry(entry)
				scope := entry.tables.targets
				found := false
				for _, g := range scope.tailActive {
					for i, owner := range g.owners {
						if owner == producer {
							counts, _, err := rv64NativeTailOwnerCounts(owner)
							if err != nil || counts.Functions != 12 || counts.MaskSlots != 12 || counts.NonCallable != 2 || counts.Values != 19 {
								t.Fatal("actual sparse generation counts", counts, err)
							}
							if i >= len(g.snapshots) || g.snapshots[i] == nil || g.snapshots[i].Validate() != nil {
								t.Fatal("actual sparse snapshot")
							}
							found = true
						}
					}
				}
				if !found {
					t.Fatal("closed producer missing from uncached generation")
				}
			}()
			for _, kind := range []string{"exact", "code-below", "metadata-below"} {
				limited := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				t.Cleanup(func() { _ = limited.Close() })
				in := rv64NativeTokenInstantiate(t, limited, cc, imports)
				code, metadata, base, restore := rv64ConfigureDynamicTailQuota(t, in, kind)
				codeLimit, metadataLimit := uint64(code), base+uint64(metadata)
				if kind == "code-below" {
					codeLimit--
				}
				if kind == "metadata-below" {
					metadataLimit--
				}
				args := []uint64{37, 0x7ff0000000000001}
				if vector {
					args = append(args, 0x80000000deadbeef)
				}
				want := append(slices.Clone(args[1:]), 46, 0)
				got, err := in.Invoke("call", args...)
				restore()
				if kind == "exact" {
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("exact generation quota", got, err)
					}
				} else {
					var quota *coreruntime.ResourceLimitError
					resource, requested, limit := "native code bytes", uint64(code), codeLimit
					if kind == "metadata-below" {
						resource, requested, limit = "instance metadata bytes", base+uint64(metadata), metadataLimit
					}
					if !errors.As(err, &quota) || quota.Resource != resource || quota.Requested != requested || quota.Limit != limit {
						t.Fatal("sparse exact rejection", kind, err)
					}
				}
				for _, owner := range []*Instance{in, producer} {
					s := &owner.pluginState.Load().railshotInvocation
					if s.tailGenerationBudget.count != 0 || s.definedTailOwner != nil {
						t.Fatal("generation charge not refunded")
					}
				}
				in.Close()
				limited.Close()
			}
		})
	}
}

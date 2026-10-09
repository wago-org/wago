//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func TestRV64PublicReferenceFunctionImportedStartAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-imported-start-admission", wasmtest.ClosedReferenceImportedStart(vector, true), false)
		for name, change := range map[string]func(*Compiled){"negative": func(v *Compiled) { v.StartImportIdx = -1 }, "missing": func(v *Compiled) { v.StartImportIdx = 3 }, "nonvoid": func(v *Compiled) { v.StartImportIdx = 0 }, "late-nonvoid": func(v *Compiled) { v.StartImportIdx = 2 }, "local-inactive": func(v *Compiled) { v.StartLocalFunc = 1 }, "no-start-kind": func(v *Compiled) { v.HasStart = false }, "defined-inactive": func(v *Compiled) { v.StartIsImport = false }, "private-import": func(v *Compiled) { v.importFuncSigs[1] = v.Funcs[1] }, "wrong-binding": func(v *Compiled) { v.importFuncSigs[1].TypeIndex = 2 }, "missing-binding": func(v *Compiled) { v.importFuncSigs[1].HasTypeIndex = false }} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				v := rv64ReferenceStartCopy(c)
				change(v)
				if err := v.validate(); err == nil {
					t.Fatal("malformed imported start admitted")
				}
				if name == "local-inactive" || name == "no-start-kind" || name == "defined-inactive" {
					return
				}
				blob, err := marshalCompiled(v)
				if err != nil {
					return
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("malformed imported start artifact loaded")
				}
			})
		}
		owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
		defer rv64HostCertificateCloseRuntime(t, owners)
		provider, _, _, im, _ := rv64ReferenceStartProvider(t, owners, false, vector, true, false)
		calls := 0
		im.Function("env.calls", "unused.fn", func(v int32) int32 { return v }).Function("env.calls", "finish.fn", func(v int32) int32 { return v }).Function("env.calls", "start.fn", func() { calls++ })
		roots := provider.referenceLifetime().snapshot().ResourceRoots
		rt := NewRuntime()
		mod, err := rt.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithImports(im))
		mod.Close()
		rt.Close()
		if in != nil {
			in.Close()
		}
		if in != nil || !errors.Is(err, railshot.ErrUnsupported) || calls != 0 || c.loadCodeCache().sealed || c.loadCodeCache().refs != 0 || provider.referenceLifetime().snapshot().ResourceRoots != roots {
			t.Fatal("default destination published imported private start", err)
		}
	}
}

func TestRV64PublicReferenceFunctionImportedStartLinkingAndInitializer(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, behavior := range []string{"missing", "signature", "closed-owned", "closed-native", "initializer"} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-%s", artifact, vector, behavior), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					provider, _, memories, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, true, false)
					source := wasmtest.ClosedReferenceImportedStart(vector, true)
					if behavior == "initializer" {
						source = wasmtest.ClosedReferenceImportedStartBadData(vector, true)
					}
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-imported-start-link-v%t-%s", vector, behavior), source, artifact)
					calls := 0
					var start any = func() { calls++ }
					if behavior == "signature" {
						start = func(int32) { calls++ }
					}
					if behavior == "closed-native" {
						nc := rv64NativeTailCompile(t, "reference-imported-start-native-sfalse", rv64PublicNativeStartProducerModule(false, nil), artifact)
						flag, err := provider.ExportedGlobalObject("counter")
						if err != nil {
							t.Fatal(err)
						}
						native := rv64NativeTokenInstantiate(t, rt, nc, NewImports().Global("env", "flag", flag).Function("env", "host", func() { calls++ }))
						target, err := native.ExportedFunc("producer_boot")
						if err != nil {
							t.Fatal(err)
						}
						start = target
						if err := native.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if behavior == "closed-owned" {
						start = rv64DynamicHostBinding(t, rt, true, func() { calls++ }, pc.importFuncSigs[1])
						if err := start.(*HostFuncRef).Close(); err != nil {
							t.Fatal(err)
						}
					}
					im.Function("env.calls", "unused.fn", func(v int32) int32 { return v }).Function("env.calls", "finish.fn", func(v int32) int32 { return v })
					if behavior != "missing" {
						im.Function("env.calls", "start.fn", start)
					}
					mod, err := rt.Module(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					roots := provider.referenceLifetime().snapshot().ResourceRoots
					cache := pc.loadCodeCache()
					refs, base, sealed := cache.refs, cache.base, cache.sealed
					for attempt := range 2 {
						in, err := rt.Instantiate(context.Background(), mod, WithImports(im))
						if in != nil {
							in.Close()
							t.Fatal("failed linking/initializer returned instance")
						}
						if err == nil {
							t.Fatal("failed linking/initializer succeeded")
						}
						if behavior == "missing" && !errors.Is(err, ErrMissingImport) {
							t.Fatal(err)
						}
						if behavior == "initializer" && !strings.Contains(err.Error(), "active data segment 0 out of bounds on memory 0: offset 65536 + length 1 > memory size 65536") {
							t.Fatal(err)
						}
						if behavior != "initializer" && (cache.base != base || cache.sealed != sealed) {
							t.Fatal("failed linking published executable code", attempt)
						}
						if calls != 0 || provider.referenceLifetime().snapshot().ResourceRoots != roots || pc.loadCodeCache().refs != refs || readGlobalObject(provider.globalCells[1], ValI32) != 11 || memories[0].UnsafeBytes()[49] != 0 {
							t.Fatal("failed imported start leaked roots/effects", attempt)
						}
					}
				})
			}
		}
	}
}

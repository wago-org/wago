//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func rv64DynamicTailCharge(t *testing.T, in *Instance) (code, metadata int, base uint64) {
	t.Helper()
	gate := in.lockInvocation(0)
	defer gate.unlockInvocation()
	entry, err := in.beginNativeEntry()
	if err != nil {
		t.Fatal(err)
	}
	defer in.unlockNativeEntry(entry)
	scope := entry.tables.targets
	if scope == nil || len(scope.tailActive) == 0 {
		t.Fatal("missing actual generation")
	}
	return scope.tailBudget.code, scope.tailBudget.metadata, in.pluginState.Load().railshotInvocation.tailPolicy.metadataBase
}

func TestRV64DynamicTailStaticImporterAggregatePolicy(t *testing.T) {
	pc := rv64NativeTailCompile(t, "dynamic-static-policy-producer", rv64PublicDynamicTailModule([]wasm.ValType{wasm.I32}, false), false)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	c := rv64NativeTailCompile(t, "dynamic-static-policy-root", rv64PublicTailModule([]wasm.ValType{wasm.I32}, false), false)
	imports := testImports("env.tail", export)
	root, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	code, metadata, base := rv64DynamicTailCharge(t, root)
	static := railshotNativeCodeBytes(c) - uint64(len(c.code))
	if static == 0 || uint64(code) <= static || root.pluginState.Load().railshotInvocation.nativeImports == nil {
		t.Fatal("fixture lacks retained static dispatcher/wrapper")
	}
	for _, limits := range []struct{ code, metadata uint64 }{{uint64(code), base + uint64(metadata)}, {uint64(code) - static, base + uint64(metadata)}, {uint64(code), base + uint64(metadata) - 1}} {
		rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithMaxNativeCodeBytes(limits.code).WithMaxInstanceMetadataBytes(limits.metadata)))
		defer rt.Close()
		in := rv64HostTailInstantiate(t, rt, c, imports)
		got, err := in.Invoke("call", 0xcafe123480000001)
		if limits.code == uint64(code) && limits.metadata == base+uint64(metadata) {
			if err != nil || len(got) != 1 || got[0] != 0x80000001 {
				t.Fatal("exact aggregate static/dynamic quota", got, err)
			}
		} else {
			var quota *coreruntime.ResourceLimitError
			if !errors.As(err, &quota) {
				t.Fatal("undercharged static publication or metadata", err)
			}
			if limits.code < uint64(code) && (quota.Resource != "native code bytes" || quota.Requested != uint64(code) || quota.Limit != limits.code) {
				t.Fatal("static dispatcher omitted from exact aggregate", quota)
			}
		}
		if producer.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 {
			t.Fatal("failed/completed scope retained caller reservation")
		}
	}
}

func TestRV64DynamicTailConfiguredPolicy(t *testing.T) {
	source := rv64PublicDynamicTailModule([]wasm.ValType{wasm.I32}, false)
	c := rv64NativeTailCompile(t, "dynamic-tail-policy", source, false)
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	code, metadata, base := rv64DynamicTailCharge(t, in)
	for _, destination := range []bool{false, true} {
		for _, kind := range []string{"exact", "code-below", "metadata-below", "zero-remainder", "unbounded", "large"} {
			t.Run(fmt.Sprintf("destination%t-%s", destination, kind), func(t *testing.T) {
				codeLimit, metadataLimit := uint64(code), base+uint64(metadata)
				fail := ""
				switch kind {
				case "code-below":
					codeLimit--
					fail = "native code bytes"
				case "metadata-below":
					metadataLimit--
					fail = "instance metadata bytes"
				case "zero-remainder":
					metadataLimit = base
					fail = "instance metadata bytes"
				case "unbounded":
					codeLimit = 0
					metadataLimit = 0
				case "large":
					codeLimit = ^uint64(0)
					metadataLimit = ^uint64(0)
				}
				cfg := rv64NativeTailConfig().WithMaxNativeCodeBytes(codeLimit).WithMaxInstanceMetadataBytes(metadataLimit)
				var instance *Instance
				if destination {
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					instance = rv64HostTailInstantiate(t, rt, c, nil)
				} else {
					compiled, e := cfg.Compile(source)
					if e != nil {
						t.Fatal(e)
					}
					defer compiled.Close()
					instance, e = Instantiate(compiled)
					if e != nil {
						t.Fatal(e)
					}
					defer instance.Close()
				}
				got, e := instance.Invoke("call", uint64(42), uint64(0))
				if fail == "" {
					if e != nil || len(got) != 1 || got[0] != 42 {
						t.Fatal(got, e)
					}
				} else {
					var quota *coreruntime.ResourceLimitError
					if !errors.As(e, &quota) || quota.Resource != fail {
						t.Fatal("missing configured quota", e)
					}
					expected, limit := uint64(code), codeLimit
					if fail == "instance metadata bytes" {
						expected, limit = base+uint64(metadata), metadataLimit
					}
					if quota.Requested != expected || quota.Limit != limit {
						t.Fatal("wrong exact charge", quota, expected, limit)
					}
				}
				state := &instance.pluginState.Load().railshotInvocation
				if state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || rv64EntryCapability(instance) != 0 {
					t.Fatal("failed or completed call retained generation")
				}
			})
		}
	}
	// Destination authority can be both stronger and weaker than compile-origin
	// authority without mutating another instance of the same compiled image.
	lowCfg := rv64NativeTailConfig().WithMaxNativeCodeBytes(uint64(code - 1)).WithMaxInstanceMetadataBytes(base + uint64(metadata-1))
	low, err := lowCfg.Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer low.Close()
	standalone, err := Instantiate(low)
	if err != nil {
		t.Fatal(err)
	}
	defer standalone.Close()
	rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
	defer rt.Close()
	destination := rv64HostTailInstantiate(t, rt, low, nil)
	if got, err := destination.Invoke("call", 42, 0); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatal("destination policy was replaced by origin", got, err)
	}
	var quota *coreruntime.ResourceLimitError
	if _, err := standalone.Invoke("call", 42, 0); !errors.As(err, &quota) {
		t.Fatal("destination widened origin policy", err)
	}
	blob, err := low.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	decoded, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	policy := decoded.pluginState.Load().railshotInvocation.tailPolicy
	if policy.codeConfigured || policy.metadataConfigured || policy.limits != (rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}) {
		t.Fatal("artifact carried allocation authority", policy)
	}
	if got, err := decoded.Invoke("call", 42, 0); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatal("decoded unbounded policy", got, err)
	}
}

func TestRV64DynamicTailForeignCallerPolicy(t *testing.T) {
	source := rv64PublicDynamicTailModule([]wasm.ValType{wasm.I32}, false)
	pc := rv64NativeTailCompile(t, "dynamic-tail-foreign-policy", source, false)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	code, _, _ := rv64DynamicTailCharge(t, producer)
	limited, err := rv64NativeTailConfig().WithMaxNativeCodeBytes(uint64(code - 1)).Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	producer, err = Instantiate(limited)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	table, err := producer.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	cc := rv64NativeTailCompile(t, "dynamic-tail-foreign-policy-caller", rv64PublicDynamicTailModule([]wasm.ValType{wasm.I32}, true), false)
	caller, err := Instantiate(cc, NewImports().Table("env", "table", table))
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	var quota *coreruntime.ResourceLimitError
	if _, err := caller.Invoke("call", 42, 0); !errors.As(err, &quota) || quota.Resource != "native code bytes" || quota.Limit != uint64(code-1) {
		t.Fatal("entry widened producer authority", err)
	}
	for _, in := range []*Instance{caller, producer} {
		if in.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
			t.Fatal("partial graph allocation was not retired")
		}
	}
}

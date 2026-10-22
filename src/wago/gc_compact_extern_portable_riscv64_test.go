//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"os"
	"testing"
)

func TestRV64CompactExternCompilerFreePrivate(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_COMPACT_EXTERN_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("private artifact consumer")
	}
	for _, vector := range []bool{false, true} {
		for _, name := range []string{"bulk", "passive", "constant", "aggregate-struct", "aggregate-array", "exception", "host-bulk", "host-cycle", "constant-struct", "constant-array", "host-global", "host-global-cycle", "plain-reentry", "function-signature"} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("%s/vector%t/profile%d", name, vector, profile), func(t *testing.T) {
					data, err := os.ReadFile(fmt.Sprintf("%s/%s-%t.wago", dir, name, vector))
					if err != nil {
						t.Fatal(err)
					}
					c, err := LoadTrustedArtifact(data)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(c)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					imports := NewImports()
					var foreign ExternRef
					var seed *Global
					var hostTable *Table
					var hostGlobal *Global
					if name == "host-bulk" || name == "host-cycle" || name == "plain-reentry" {
						hostTable, err = rt.NewExternRefTable(4, 8)
						if err != nil {
							t.Fatal(err)
						}
						defer hostTable.Close()
						imports.Table("env", "table", hostTable)
					}
					if name == "host-global" || name == "host-global-cycle" {
						hostGlobal, err = rt.NewExternRefGlobal(ExternRef{}, true)
						if err != nil {
							t.Fatal(err)
						}
						defer hostGlobal.Close()
						imports.Global("env", "value", hostGlobal)
					}
					if name == "plain-reentry" {
						imports.Function("env", "collect", slotHostFunc(func(host HostModule, _ []uint64, _ []uint64) {
							if err := host.(GCHostModule).CollectGC(); err != nil {
								panic(err)
							}
						}))
					}
					if name == "passive" {
						foreign, err = rt.NewExternRef("compiler-free passive")
						if err != nil {
							t.Fatal(err)
						}
						seed, err = rt.NewExternRefGlobal(foreign, false)
						if err != nil {
							t.Fatal(err)
						}
						defer seed.Close()
						imports.Global("env", "value", seed)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(imports), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					invoke := func(name string, args ...uint64) []uint64 {
						t.Helper()
						got, err := in.Invoke(name, args...)
						if err != nil {
							t.Fatal(name, err)
						}
						return got
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					switch name {
					case "function-signature":
						producerData, err := os.ReadFile(fmt.Sprintf("%s/function-producer-%t.wago", dir, vector))
						if err != nil {
							t.Fatal(err)
						}
						pc, err := LoadTrustedArtifact(producerData)
						if err != nil {
							t.Fatal(err)
						}
						defer pc.Close()
						pm, err := rt.Module(pc)
						if err != nil {
							t.Fatal(err)
						}
						defer pm.Close()
						producer, err := rt.Instantiate(context.Background(), pm)
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						value, err := producer.Invoke("function")
						if err != nil {
							t.Fatal(err)
						}
						if err := producer.Close(); err != nil {
							t.Fatal(err)
						}
						if !c.hasFunctionEnvironmentFrames() || !c.functionEnvironmentFrameRoots() {
							t.Fatal("function frame authority not restored")
						}
						for i := 0; i < 8; i++ {
							got := invoke("run", value[0])
							if len(got) != 1 || got[0] != 42 {
								t.Fatal("function frame after collection", got)
							}
						}
					case "host-global":
						invoke("save", 42)
						collector := in.gc
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
						if in.hasPhysicalResources() {
							t.Fatal("host global pinned first importer")
						}
						second, err := rt.Instantiate(context.Background(), mod, WithImports(imports), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer second.Close()
						if second.gc != collector {
							t.Fatal("global collector domain changed")
						}
						if err := second.CollectGC(); err != nil {
							t.Fatal(err)
						}
						got, err := second.Invoke("read")
						if err != nil || len(got) != 1 || got[0] != 42 {
							t.Fatal("host global after first close", got, err)
						}
						public, err := hostGlobal.GetValue()
						if err != nil {
							t.Fatal(err)
						}
						if err := second.Close(); err != nil {
							t.Fatal(err)
						}
						if err := hostGlobal.Close(); err != nil {
							t.Fatal(err)
						}
						if rt.refStore.gcDomainForCollector(collector) == nil {
							t.Fatal("public global token lost domain")
						}
						if !rt.ReleaseExternRef(ExternRef{public.bits}) || rt.refStore.gcDomainForCollector(collector) != nil {
							t.Fatal("global public token claim not released")
						}
					case "host-global-cycle":
						invoke("save")
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
						if err := hostGlobal.Close(); err != nil {
							t.Fatal(err)
						}
						if in.hasPhysicalResources() || compactExternHostGlobalRow(hostGlobal.owner) != nil {
							t.Fatal("compiler-free host global cycle retained")
						}
					case "plain-reentry":
						producerData, err := os.ReadFile(fmt.Sprintf("%s/host-bulk-%t.wago", dir, vector))
						if err != nil {
							t.Fatal(err)
						}
						pc, err := LoadTrustedArtifact(producerData)
						if err != nil {
							t.Fatal(err)
						}
						defer pc.Close()
						pm, err := rt.Module(pc)
						if err != nil {
							t.Fatal(err)
						}
						defer pm.Close()
						producer, err := rt.Instantiate(context.Background(), pm, WithImports(NewImports().Table("env", "table", hostTable)), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						for i := 0; i < 8; i++ {
							value, err := producer.Invoke("make", 42)
							if err != nil {
								t.Fatal(err)
							}
							if _, err := producer.Invoke("fill", 0, value[0], 1); err != nil {
								t.Fatal(err)
							}
							if !rt.ReleaseExternRef(ExternRef{value[0]}) {
								t.Fatal("release producer token")
							}
							got := invoke("get", 0)
							if len(got) != 1 || got[0] == 0 {
								t.Fatal("parked extern local", got)
							}
							if !rt.ReleaseExternRef(ExternRef{got[0]}) {
								t.Fatal("release parked local result")
							}
						}
					case "aggregate-struct", "aggregate-array", "exception":
						for i := uint64(0); i < 16; i++ {
							if got := invoke("run", i+42); len(got) != 1 || got[0] != i+42 {
								t.Fatal("opaque payload across collection", got)
							}
						}
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
						state, ok := compactExternCollectors.Load(in.gc)
						if !ok || len(state.shells) != 0 || len(state.foreign) != 0 {
							t.Fatal("dead payload retained extern metadata")
						}
					case "constant-struct", "constant-array":
						if got := invoke("run"); len(got) != 1 || got[0] != 42 {
							t.Fatal("aggregate constant", got)
						}
					case "host-cycle":
						invoke("save")
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
						if err := hostTable.Close(); err != nil {
							t.Fatal(err)
						}
						in.lifeMu.Lock()
						closed := in.resourcesClosed
						in.lifeMu.Unlock()
						if !closed || compactExternHostTableRow(hostTable.owner) != nil {
							t.Fatal("compiler-free host cycle retained")
						}
					case "constant":
						if got := invoke("first"); len(got) != 1 || got[0] != 42 {
							t.Fatal("first constant", got)
						}
						if got := invoke("second"); len(got) != 1 || got[0] != 7 {
							t.Fatal("second constant", got)
						}
					case "bulk", "host-bulk":
						token := invoke("make", 42)[0]
						invoke("fill", 0, token, 4)
						if !rt.ReleaseExternRef(ExternRef{token}) {
							t.Fatal("release public converted token")
						}
						invoke("copy", 1, 0, 3)
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
						if got := invoke("read", 3); len(got) != 1 || got[0] != 42 {
							t.Fatal("bulk result", got)
						}
						invoke("grow", 0, 2)
						invoke("clear")
					case "passive":
						invoke("init", 0, 0, 3)
						invoke("drop")
						if got := invoke("get", 2); len(got) != 1 || got[0] != foreign.token {
							t.Fatal("passive result", got)
						}
						invoke("fill", 0, 0, 4)
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
						if err := seed.Close(); err != nil {
							t.Fatal(err)
						}
						if !rt.ReleaseExternRef(foreign) {
							t.Fatal("compiler-free passive retained pins")
						}
					}
				})
			}
		}
	}
}

func TestRV64CompactExternLegacyImagePrivate(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_COMPACT_EXTERN_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("private legacy artifact compatibility")
	}
	data, err := os.ReadFile(dir + "/legacy-extern-echo.wago")
	if err != nil {
		t.Fatal(err)
	}
	c, err := LoadTrustedArtifact(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.nativeGCABIRequirement() != 0 || c.genericGCFrameRoots() != nil || c.codeCache.gcMetadataFlags&0xc0 != 0 {
		t.Fatal("legacy extern artifact gained frame authority")
	}
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gc != nil {
		t.Fatal("legacy echo gained collector")
	}
	foreign, err := rt.NewExternRef("legacy ordinary extern")
	if err != nil {
		t.Fatal(err)
	}
	got, err := in.Invoke("echo", foreign.token)
	if err != nil || len(got) != 1 || got[0] != foreign.token {
		t.Fatal("legacy extern echo", got, err)
	}
	if !rt.ReleaseExternRef(foreign) {
		t.Fatal("legacy token retained")
	}
}

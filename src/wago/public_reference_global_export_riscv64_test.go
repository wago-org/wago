//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceGlobalExportModule(t *testing.T, vector, imported, null bool, count int) []byte {
	t.Helper()
	source := rv64ReferenceGlobalModule(vector, false, imported, null, count)
	return rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("isnull", 0, 3), wasmtest.ExportEntry("reference", 3, uint32(count-1)), wasmtest.ExportEntry("alias", 3, uint32(count-1)))))
}

func TestRV64PublicReferenceGlobalExports(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, null := range []bool{false, true} {
			for _, managed := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-null%t-managed%t-artifact%t", vector, null, managed, artifact), func(t *testing.T) {
						// Use the final supported global slot on the vector path.
						count := 1
						if vector {
							count = 256
						}
						c := rv64TableStageCompile(t, fmt.Sprintf("ref-export-vector%t-null%t", vector, null), rv64ReferenceGlobalExportModule(t, vector, false, null, count), artifact)
						var in *Instance
						var rt *Runtime
						var err error
						if managed {
							rt = NewRuntime()
							defer rt.Close()
							mod, e := rt.Module(c)
							if e != nil {
								t.Fatal(e)
							}
							defer mod.Close()
							in, err = rt.Instantiate(context.Background(), mod)
							mod.Close()
						} else {
							in, err = Instantiate(c)
						}
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						c.Close()
						runtime.GC()
						first, err := in.GlobalValue("reference")
						if err != nil || first.Type() != ValFuncRef || first.FuncRef().IsNull() != null {
							t.Fatal("reference export", first, err)
						}
						if !null && !in.FuncRefMatchesFunction(first.FuncRef(), 0) {
							t.Fatal("local function token identity")
						}
						g, err := in.ExportedGlobalObject("reference")
						if err != nil {
							t.Fatal(err)
						}
						alias, err := in.ExportedGlobalObject("alias")
						if err != nil || alias != g {
							t.Fatal("global export alias owner", err)
						}
						for range 3 {
							if got, err := in.GlobalValue("alias"); err != nil || got != first {
								t.Fatal("alias token identity", got, err)
							}
							if got, err := g.GetValue(); err != nil || got != first {
								t.Fatal("global object token identity", got, err)
							}
						}
						if _, err := in.Global("reference"); err == nil {
							t.Fatal("raw instance reference read")
						}
						if g.Get() != 0 || g.GetV128() != (V128{}) {
							t.Fatal("raw global object exposed descriptor")
						}
						if in.SetGlobal("reference", 1) == nil || in.SetGlobalValue("reference", first) == nil || g.Set(1) == nil || g.SetValue(first) == nil {
							t.Fatal("immutable reference write accepted")
						}
						if !null {
							descriptor := readGlobalObject(g, ValFuncRef)
							if first.Bits() == descriptor {
								t.Fatal("token equals raw descriptor")
							}
							before := len(in.refStore.byToken)
							for _, bad := range []uint64{1, descriptor + 1} {
								writeGlobalObject(g, ValFuncRef, bad)
								if _, err := in.GlobalValue("reference"); err == nil {
									t.Fatal("forged descriptor tokenized")
								}
								if _, err := g.GetValue(); err == nil {
									t.Fatal("forged global object descriptor tokenized")
								}
							}
							writeGlobalObject(g, ValFuncRef, descriptor)
							if len(in.refStore.byToken) != before || before != 1 {
								t.Fatal("unstable or forged token allocation", before, len(in.refStore.byToken))
							}
							foreign := NewRuntime()
							defer foreign.Close()
							rejected, err := foreign.NewFuncRefGlobal(first.FuncRef(), false)
							if rejected != nil {
								rejected.Close()
							}
							if err == nil {
								t.Fatal("cross-store token accepted")
							}
						}
						var held *Global
						if managed {
							held, err = rt.NewFuncRefGlobal(first.FuncRef(), false)
							if err != nil {
								t.Fatal(err)
							}
							defer held.Close()
						}
						in.Close()
						if _, err := in.GlobalValue("reference"); err == nil {
							t.Fatal("closed instance global access")
						}
						if _, err := g.GetValue(); err == nil {
							t.Fatal("closed producer global object access")
						}
						if managed {
							if got, err := held.GetValue(); err != nil || got != first {
								t.Fatal("retained host object token", got, err)
							}
							if !null && !in.hasPhysicalResources() {
								t.Fatal("token owner retired early")
							}
							if err := held.Close(); err != nil {
								t.Fatal(err)
							}
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							if err := rt.CloseContext(ctx); err != nil {
								t.Fatal(err)
							}
						}
						if in.hasPhysicalResources() || c.codeCache.mem != nil {
							t.Fatal("reference export mapping retained after final store close")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceGlobalExportImportedPrefix(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				source := rv64ReferenceGlobalExportModule(t, vector, true, false, 1)
				// Keep the unused Go import scalar in both compiler profiles.
				source = rv64ReplaceTableStageSection(t, source, 2, wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 4))))
				source = rv64ReplaceTableStageSection(t, source, 6, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 1, 0x0b}))))
				c := rv64TableStageCompile(t, fmt.Sprintf("ref-export-prefix-vector%t", vector), source, artifact)
				in, err := Instantiate(c, NewImports().Function("env", "host", slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p) })))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				c.Close()
				value, err := in.GlobalValue("reference")
				if err != nil || value.FuncRef().IsNull() || !in.FuncRefMatchesFunction(value.FuncRef(), 1) || in.FuncRefMatchesFunction(value.FuncRef(), 0) {
					t.Fatal("defined function identity after imported prefix", value, err)
				}
				importedGlobalInvoke(t, in, "isnull", []uint64{0})
			})
		}
	}
}

func TestRV64PublicReferenceGlobalExportImportGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source and artifact metadata gate")
	}
	for _, vector := range []bool{false, true} {
		source := rv64ReferenceGlobalExportModule(t, vector, true, false, 1)
		if c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), source); c != nil || err == nil {
			if c != nil {
				c.Close()
			}
			t.Fatal("unqualified imported proxy exported", vector, err)
		}
		c := rv64TableStageCompile(t, fmt.Sprintf("ref-export-import-gate-vector%t", vector), rv64ReferenceGlobalModule(vector, false, true, false, 1), false)
		v := *c.executionView()
		v.GlobalExports = map[string]int{"reference": 0}
		if err := railshotValidateCompiled(&v); err == nil {
			t.Fatal("unqualified imported proxy metadata")
		}
		blob, err := marshalCompiled(&v)
		if err != nil {
			continue
		}
		loaded, err := LoadTrustedArtifact(blob)
		if loaded != nil {
			loaded.Close()
		}
		if err == nil {
			t.Fatal("unqualified imported proxy artifact")
		}
	}
}

//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func rv64ImportCertificateFixture(t *testing.T, vector, artifact bool) (*Instance, *Instance, *Compiled, *Imports) {
	t.Helper()
	types := []wasm.ValType{wasm.I32}
	if vector {
		types = append(types, wasm.V128)
	}
	key := fmt.Sprintf("native-import-certificate-%t", vector)
	pc := rv64NativeTailCompile(t, key+"-producer", rv64PublicNativeIdentityModule(types), artifact)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close() })
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	imports := NewImports().Function("env", "host", export)
	c := rv64NativeTailCompile(t, key+"-caller", rv64TableFunctionImportModule(types, false, false, nil), artifact)
	in, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in, producer, c, imports
}

func TestRV64NativeImportCertificateActualMappingAndLease(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				in, producer, c, imports := rv64ImportCertificateFixture(t, vector, artifact)
				s := in.pluginState.Load().railshotInvocation.nativeImports
				if s == nil || !s.published || len(s.records) != 1 || s.codeBytes <= 0 || s.codeBytes > len(in.thunkMem) || s.base != offHeapSlicePtr(in.thunkMem) || s.mappingBytes != len(in.thunkMem) {
					t.Fatal("missing actual separate mapping owner")
				}
				if len(producer.funcRefDescs) != coreruntime.FuncRefDescBytes {
					t.Fatal("fixture did not use descriptor-zero-only producer")
				}
				owner := rv64TailThunkCodeOwner{in}
				if image, err := railshot.NewNativeTailCodeImage(owner); image != nil || !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("unleased importer image accepted", err)
				}
				if !in.retainResourceRoot() {
					t.Fatal("importer physical root")
				}
				root := true
				defer func() {
					if root {
						in.releaseResourceRoot()
					}
				}()
				image, err := railshot.NewNativeTailCodeImage(owner)
				if err != nil {
					t.Fatal(err)
				}
				record := &s.records[0]
				nativeExecutionMu.Lock()
				err = record.validate(in, 0, image)
				nativeExecutionMu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				input := []uint64{33}
				if vector {
					input = append(input, 0x1234567887654321, 0xfedcba9876543210)
				}
				got, err := in.Invoke("target", input...)
				if err != nil || len(got) != len(input) || got[0] != 42 {
					t.Fatal("ordinary bridge execution changed", got, err)
				}
				nativeExecutionMu.Lock()
				allocations := testing.AllocsPerRun(4, func() {
					if err := record.validate(in, 0, image); err != nil {
						panic(err)
					}
				})
				nativeExecutionMu.Unlock()
				if allocations != 0 {
					t.Fatal("warm import provenance allocated", allocations)
				}
				for _, kind := range []string{"publication", "base", "mapped-length", "logical-length", "span", "transport", "producer"} {
					saved := *s
					savedRecord := *record
					switch kind {
					case "publication":
						s.published = false
					case "base":
						s.base += 16
					case "mapped-length":
						s.mappingBytes--
					case "logical-length":
						s.codeBytes -= 4
					case "span":
						record.codeBytes -= 4
					case "transport":
						record.target.Arguments = record.target.Results
					case "producer":
						record.producer = in
					}
					nativeExecutionMu.Lock()
					err = record.validate(in, 0, image)
					nativeExecutionMu.Unlock()
					*s = saved
					*record = savedRecord
					if !errors.Is(err, railshot.ErrUnsupported) {
						t.Fatal("changed provenance accepted", kind, err)
					}
				}
				other, err := Instantiate(c, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()

				if !other.retainResourceRoot() {
					t.Fatal("other importer physical root")
				}
				defer other.releaseResourceRoot()
				otherImage, err := railshot.NewNativeTailCodeImage(rv64TailThunkCodeOwner{other})
				if err != nil {
					t.Fatal(err)
				}
				nativeExecutionMu.Lock()
				err = record.validate(in, 0, otherImage)
				nativeExecutionMu.Unlock()
				if !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("foreign importer image accepted", err)
				}
				original := in.thunkMem
				in.thunkMem = other.thunkMem
				if _, err := owner.NativeTailCodeView(); !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("another instance mapping accepted", err)
				}
				in.thunkMem = original
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				nativeExecutionMu.Lock()
				err = record.validate(in, 0, image)
				nativeExecutionMu.Unlock()
				if err != nil {
					t.Fatal("logical close retired a physically owned bridge", err)
				}
				in.releaseResourceRoot()
				root = false
				if in.pluginState.Load().railshotInvocation.nativeImports != nil || !errors.Is(image.Validate(), railshot.ErrUnsupported) {
					t.Fatal("retired instance retained certificate transport")
				}
				nativeExecutionMu.Lock()
				err = record.validate(in, 0, otherImage)
				nativeExecutionMu.Unlock()
				if !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("live foreign image authorized retired importer", err)
				}

			})
		}
	}
}

func TestRV64NativeImportCertificateMetadataAdmission(t *testing.T) {
	in, producer, c, imports := rv64ImportCertificateFixture(t, false, false)
	cost := railshotNativeImportMetadataBytes(in.c, in.imports)
	base := in.c.arenaNeedForImports(in.imports, in.syncMode)
	need := uint64(base + cost)
	if cost == 0 || in.pluginState.Load().railshotInvocation.tailPolicy.metadataBase != need {
		t.Fatal("persistent certificate metadata not charged", cost, need)
	}
	for _, limit := range []uint64{need - 1, need} {
		rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithMaxInstanceMetadataBytes(limit)))
		mod, err := rt.Module(c)
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		before := producer.referenceLifetime().snapshot().ResourceRoots
		got, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if limit < need {
			var quota *coreruntime.ResourceLimitError
			if got != nil || !errors.As(err, &quota) || quota.Requested != need || quota.Limit != limit || producer.referenceLifetime().snapshot().ResourceRoots != before {
				t.Fatal("certificate quota did not reject before attachment", err)
			}
		} else if err != nil {
			t.Fatal("exact certificate quota", err)
		}
		if got != nil {
			got.Close()
		}
		mod.Close()
		rt.Close()
	}
}

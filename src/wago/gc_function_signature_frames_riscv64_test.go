//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"context"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"

	"testing"
)

func rv64FunctionSignatureFrameSource(vector bool) []byte {
	body := []byte{0x20, 0, 0x41, 1, 0xfb, 0, 1, 0x1a, 0x14, 0, 0x0b}
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		body = append(append(prefix, 0x1a), body...)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, []byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x63, 0, 1, 0x7f})), wasmtest.Section(3, []byte{1, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func rv64FunctionSignatureProducerSource() []byte {
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, []byte{0x60, 0, 1, 0x63, 0})), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("function", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0xd2, 0, 0x0b}))))
}

func TestRV64FunctionSignatureFrameAuthority(t *testing.T) {
	for _, vector := range []bool{false, true} {
		source, err := NewRuntimeConfig().Compile(rv64FunctionSignatureFrameSource(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		if !source.hasFunctionEnvironmentFrames() || !source.functionEnvironmentFrameRoots() || source.nativeGCABIRequirement() != gc.NativeABIVersion {
			t.Fatalf("missing managed function signature frame authority: mode=%t managed=%t ABI=%d maps=%t struct=%d array=%d features=%#x", source.hasFunctionEnvironmentFrames(), source.usesManagedFunctionStorage(), source.nativeGCABIRequirement(), source.genericGCFrameRoots() != nil, source.stagedGCStructProduct(), source.stagedGCArrayProduct(), source.requiredFeatures)
		}
		data, err := source.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(data)
		if err != nil {
			t.Fatal(err)
		}
		defer loaded.Close()
		for artifact, candidate := range []*Compiled{source, loaded} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					producerCode := rv64FunctionSignatureProducerSource()
					producerCompiled, err := NewRuntimeConfig().Compile(producerCode)
					if err != nil {
						t.Fatal(err)
					}
					defer producerCompiled.Close()
					pm, err := rt.Module(producerCompiled)
					if err != nil {
						t.Fatal(err)
					}
					defer pm.Close()
					producer, err := rt.Instantiate(context.Background(), pm)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					result, err := producer.Invoke("function")
					if err != nil {
						t.Fatal(err)
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					cm, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer cm.Close()
					consumer, err := rt.Instantiate(context.Background(), cm, WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					for i := 0; i < 8; i++ {
						got, err := consumer.Invoke("run", result[0])
						if err != nil || len(got) != 1 || got[0] != 42 {
							t.Fatal("function signature across allocation", got, err)
						}
					}
				})
			}
		}
		for _, name := range []string{"mode", "abi", "maps"} {
			t.Run(fmt.Sprintf("vector%t/mutate-%s", vector, name), func(t *testing.T) {
				bad, err := LoadTrustedArtifact(data)
				if err != nil {
					t.Fatal(err)
				}
				defer bad.Close()
				switch name {
				case "mode":
					bad.codeCache.gcMetadataFlags &^= compiledGCMetadataFunctionFrames
				case "abi":
					bad.codeCache.setNativeGCABIVersion(0)
				case "maps":
					bad.validateMemo.gcFrameRoots = nil
				}
				if err := bad.validate(); err == nil {
					t.Fatal("mutated function signature authority admitted")
				}
			})
		}
	}
}

package wago

import (
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func extendedConstFeatureModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{
			0x41, 0x01, // i32.const 1
			0x41, 0x02, // i32.const 2
			0x6a, // i32.add
			0x0b,
		}))),
	)
}

func TestExtendedConstRespectsCoreFeatureConfiguration(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV1)
	if _, err := Compile(cfg, extendedConstFeatureModule()); err == nil || !strings.Contains(err.Error(), "extended-constant-expressions disabled") {
		t.Fatalf("MVP compile error = %v, want extended-constant feature rejection", err)
	}
	if _, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureExtendedConst, false), extendedConstFeatureModule()); err == nil || !strings.Contains(err.Error(), "extended-constant-expressions disabled") {
		t.Fatalf("explicitly disabled compile error = %v, want extended-constant feature rejection", err)
	}
	compiled, err := Compile(nil, extendedConstFeatureModule())
	if err != nil {
		t.Fatalf("default compile: %v", err)
	}
	defer compiled.Close()
	if got := CoreFeatures(compiled.requiredFeatures); got != CoreFeatureExtendedConst {
		t.Fatalf("required features = %s, want %s", got, CoreFeatureExtendedConst)
	}
}

func TestCompiledIntegerOperandBuildFeature(t *testing.T) {
	scope := constExprGlobalScope{context: constExprDataOffset, limit: 1}
	defs := []GlobalDef{{Type: ValI64}}
	globals := []*Global{NewGlobalI64(1<<40, false)}
	for _, tc := range []struct {
		name string
		body []byte
		want uint64
	}{
		{"literal", []byte{0x42, 0x7f, 0x0b}, ^uint64(0)},
		{"global", []byte{0x23, 0, 0x0b}, 1 << 40},
		{"arithmetic", []byte{0x23, 0, 0x42, 1, 0x7c, 0x0b}, 1<<40 + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validateErr := validateCompiledScalarConstExpr(tc.body, ValI64, defs, scope)
			got, evalErr := evalCompiledScalarConstExpr(tc.body, ValI64, globals, defs, scope)
			if tc.name == "arithmetic" && !buildfeatures.ExtendedConstArithmetic {
				for _, err := range []error{validateErr, evalErr} {
					var unavailable *UnsupportedFeatureError
					if !errors.As(err, &unavailable) || unavailable.Requested != CoreFeatureExtendedConst {
						t.Fatalf("error = %v, want unavailable extended constants", err)
					}
				}
				return
			}
			if validateErr != nil || evalErr != nil || got != tc.want {
				t.Fatalf("value = %#x, want %#x; validation = %v; evaluation = %v", got, tc.want, validateErr, evalErr)
			}
		})
	}
}

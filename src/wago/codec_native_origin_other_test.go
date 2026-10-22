//go:build (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func TestCompiledCodecRejectsForeignNativeOriginCapability(t *testing.T) {
	base := Compiled{Entry: []int{0}, InternalEntry: []int{0}, Funcs: []FuncSig{{}}}
	plain, err := marshalCompiledMetadata(&base)
	if err != nil {
		t.Fatal(err)
	}
	if railshotNativeContextTrapABI() {
		t.Fatal("foreign origin ABI enabled on existing target")
	}
	var decoded Compiled
	decoded.code = make([]byte, 16)
	if err := unmarshalCompiledMetadata(&decoded, plain); err != nil || decoded.nativeContextTrapABI {
		t.Fatal("existing artifact changed", err)
	}
	unchanged, err := marshalCompiledMetadata(&decoded)
	if err != nil || !bytes.Equal(plain, unchanged) {
		t.Fatal("existing-target requirement round trip changed", err)
	}
	base.nativeContextTrapABI = true
	foreign, err := marshalCompiledMetadata(&base)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreign) != len(plain) || bytes.Equal(foreign, plain) {
		t.Fatal("native origin capability was not encoded")
	}
	if err := unmarshalCompiledMetadata(&decoded, foreign); err == nil {
		t.Fatal("foreign RV64 origin capability accepted")
	}
	for _, origin := range []bool{false, true} {
		base.nativeContextTrapABI = origin
		base.nativeTableContextABI = true
		tableABI, err := marshalCompiledMetadata(&base)
		if err != nil || len(tableABI) != len(plain) {
			t.Fatal("table capability changed metadata size", err)
		}
		if err := unmarshalCompiledMetadata(&decoded, tableABI); err == nil {
			t.Fatal("foreign RV64 table capability accepted", origin)
		}
	}
	base.nativeContextTrapABI, base.nativeTableContextABI, base.nativeTableReferenceABI = true, true, true
	foreignReference, err := marshalCompiledMetadata(&base)
	if err != nil || len(foreignReference) != len(plain) {
		t.Fatal("reference capability metadata size", err)
	}
	if err := unmarshalCompiledMetadata(&decoded, foreignReference); err == nil {
		t.Fatal("foreign RV64 reference capability accepted")
	}
	base.nativeReferenceCallABI = true
	base.requiredFeatures |= CoreFeatureTypedFunctionReferences
	refCall, err := marshalCompiledMetadata(&base)
	if err != nil || len(refCall) != len(plain) {
		t.Fatal("call_ref marker metadata size", err)
	}
	if err := unmarshalCompiledMetadata(&decoded, refCall); err == nil {
		t.Fatal("foreign call_ref marker accepted")
	}
	// A lone marker must not disappear into the CPU/CoreFeatures fields.
	base.nativeContextTrapABI, base.nativeTableContextABI, base.nativeTableReferenceABI, base.nativeReferenceCallABI = false, false, false, false
	base.nativeReferenceControlABI = true
	lone, err := marshalCompiledMetadata(&base)
	if err != nil {
		t.Fatal(err)
	}
	if err := unmarshalCompiledMetadata(&decoded, lone); err == nil {
		t.Fatal("lone foreign non-null marker accepted")
	}
	base.nativeContextTrapABI, base.nativeTableContextABI, base.nativeTableReferenceABI = true, true, true
	for _, call := range []bool{false, true} {
		base.nativeReferenceCallABI = call
		blob, err := marshalCompiledMetadata(&base)
		if err != nil || len(blob) != len(plain) {
			t.Fatal("non-null marker metadata size", err)
		}
		if err := unmarshalCompiledMetadata(&decoded, blob); err == nil {
			t.Fatal("foreign non-null marker accepted", call)
		}
	}
	if err := unmarshalCompiledMetadata(&decoded, plain); err != nil || decoded.nativeReferenceControlABI || decoded.nativeReferenceCallABI || decoded.nativeContextTrapABI || decoded.nativeTableContextABI || decoded.nativeTableReferenceABI {
		t.Fatal("reused decoder retained native origin capability", err)
	}
}

func TestCompiledCodecReferenceFunctionCapability(t *testing.T) {
	base := Compiled{Entry: []int{0}, InternalEntry: []int{0}, Funcs: []FuncSig{{}}}
	plain, err := marshalCompiledMetadata(&base)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Compiled
	decoded.code = make([]byte, 16)
	for _, complete := range []bool{false, true} {
		c := base
		c.nativeReferenceFunctionABI = true
		c.nativeContextTrapABI, c.nativeTableContextABI, c.nativeTableReferenceABI, c.registerABIDisabled = complete, complete, complete, complete
		c.requiredFeatures = CoreFeatureReferenceTypes
		blob, err := marshalCompiledMetadata(&c)
		if err != nil || len(blob) != len(plain) {
			t.Fatal("function witness wire size", err)
		}
		if err := unmarshalCompiledMetadata(&decoded, blob); err == nil {
			t.Fatal("foreign closed function witness accepted", complete)
		}
	}
	if err := unmarshalCompiledMetadata(&decoded, plain); err != nil || decoded.nativeReferenceFunctionABI {
		t.Fatal("reused decoder retained function witness", err)
	}
	for _, bit := range []uint{17, 18} {
		c := base
		c.requiredAMD64Features = shared.AMD64Features(1 << bit)
		blob, err := marshalCompiledMetadata(&c)
		if err != nil {
			t.Fatal(err)
		}
		if err := unmarshalCompiledMetadata(&decoded, blob); err == nil || decoded.nativeReferenceFunctionABI {
			t.Fatal("unknown CPU bit became native execution metadata", bit, err)
		}
	}
	base.requiredAMD64Features = shared.AMD64Features(1 << 20)
	if _, err := marshalCompiledMetadata(&base); err == nil {
		t.Fatal("out-of-field CPU bit encoded")
	}
	base.requiredAMD64Features = shared.AMD64Features(1 << 16)
	unknown, err := marshalCompiledMetadata(&base)
	if err != nil {
		t.Fatal("in-field unknown CPU fixture", err)
	}
	if err := unmarshalCompiledMetadata(&decoded, unknown); err == nil || decoded.nativeReferenceFunctionABI {
		t.Fatal("unknown CPU requirement became RV64 witness", err)
	}
}

//go:build (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"testing"
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
	if err := unmarshalCompiledMetadata(&decoded, plain); err != nil || decoded.nativeContextTrapABI || decoded.nativeTableContextABI || decoded.nativeTableReferenceABI {
		t.Fatal("reused decoder retained native origin capability", err)
	}
}

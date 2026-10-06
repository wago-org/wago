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
	if err := unmarshalCompiledMetadata(&decoded, plain); err != nil || decoded.nativeContextTrapABI {
		t.Fatal("reused decoder retained native origin capability", err)
	}
}

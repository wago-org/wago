//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"testing"
)

func TestStateOnlyCodeOwnerWithoutMapping(t *testing.T) {
	c := &Compiled{validateMemo: &validateMemo{}}
	c.ensureCodeCache()
	defer c.Close()
	indexes := c.ensureCompileIndexes()
	indexes.sealCode = func([]byte) error { return errors.New("state-only seal must not run") }
	indexes.unmapCode = func([]byte) error { return errors.New("state-only unmap must not run") }
	for range 2 {
		if err := c.prepareCodeMapping(); err != nil {
			t.Fatal(err)
		}
		if base, err := c.acquireCode(); err != nil || base != 0 {
			t.Fatal("state-only acquire", base, err)
		}
	}
	if c.codeCache.mem != nil || c.codeCache.base != 0 || c.codeCache.sealed || c.codeCache.refs != 2 {
		t.Fatal("state-only owner mapped code or lost references")
	}
	if err := c.Close(); err != nil || c.codeCache.refs != 2 {
		t.Fatal("close lost active metadata owners", err)
	}
	if _, err := c.acquireCode(); err == nil {
		t.Fatal("closed owner acquired")
	}
	c.releaseCode()
	c.releaseCode()
	if c.codeCache.refs != 0 || c.codeCache.mem != nil || c.codeCache.base != 0 {
		t.Fatal("state-only final release")
	}
}

func TestStateOnlyCodeReaderAvoidsEmptyMapping(t *testing.T) {
	c := &Compiled{nativeContextTrapABI: railshotNativeContextTrapABI()}
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	decoded, image, _, err := readCompiledFrom(bytes.NewReader(blob), DefaultArtifactLimits())
	if image != nil {
		defer image.Close()
	}
	if err != nil || decoded == nil || image != nil || !decoded.hasNoFunctionCode() {
		t.Fatal("state-only reader allocated mapping", err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		defer loaded.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if loaded.codeCache.mem != nil || loaded.codeCache.base != 0 || loaded.codeCache.flags&compiledCacheWritableCode != 0 {
		t.Fatal("state-only loader published an empty mapping")
	}
	if _, err := loaded.acquireCode(); err != nil {
		t.Fatal(err)
	}
	if err := loaded.UnmarshalBinary(blob); err == nil {
		t.Fatal("replacement succeeded with a live state-only owner")
	}
	loaded.releaseCode()
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal("quiescent state-only replacement", err)
	}
}

func TestStateOnlyCodeShapeDoesNotHideFunctionMetadata(t *testing.T) {
	for _, c := range []*Compiled{
		{NumImports: 1}, {Funcs: []FuncSig{{}}}, {code: []byte{1}},
		{Entry: []int{0}}, {InternalEntry: []int{0}},
	} {
		if c.hasNoFunctionCode() {
			t.Fatal("non-state-only code owner classified as empty")
		}
	}
}

func TestImportOnlyCodeReaderKeepsMappedOwner(t *testing.T) {
	c := &Compiled{NumImports: 1, Imports: []string{"env.f"}, importFuncSigs: []FuncSig{{}}, dynamicImports: true}
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	decoded, image, _, err := readCompiledFrom(bytes.NewReader(blob), DefaultArtifactLimits())
	if image != nil {
		defer image.Close()
	}
	if err != nil || decoded == nil || image == nil || image.Base() == 0 || decoded.hasNoFunctionCode() {
		t.Fatal("import-only reader lost mapped ownership", err)
	}
}

//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
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

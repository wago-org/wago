package wago

import "testing"

func newHandBuiltCompiled(code []byte, metadata Compiled) *Compiled {
	metadata.code = code
	return &metadata
}

func compileExplicitSource(t testing.TB, module []byte) *Compiled {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

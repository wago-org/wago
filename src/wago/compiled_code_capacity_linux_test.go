//go:build linux && (amd64 || arm64) && !tinygo

package wago

import "testing"

func TestCompiledOnlyImagesDoNotReserveExecutionRanges(t *testing.T) {
	const modules = 4097 // one more than the fixed Linux execution registry
	compiled := make([]*Compiled, 0, modules)
	defer func() {
		for _, c := range compiled {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < modules; i++ {
		staged := installCompilerCompiledFinalizer(newCompilerCompiled(Compiled{code: []byte{0}}))
		c, err := publishCompilerCompiled(staged)
		if err != nil {
			t.Fatalf("publish compiled-only image %d: %v", i, err)
		}
		compiled = append(compiled, c)
	}
	// Real activation still gets a registry slot and executes correctly.
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("f", 41)
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("activation after compiled-only cache: %v, %v", got, err)
	}
}

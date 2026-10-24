//go:build (darwin || linux) && arm64

package wago

import (
	"testing"
	"unsafe"
)

func TestSerialCompileOwnsWritableImageUntilInstantiateArm64(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithFunctionWorkers(1), benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cache := c.codeCache
	if len(cache.mem) == 0 || cache.sealed {
		t.Fatal("serial compile must own an unsealed code image")
	}
	base := unsafe.SliceData(cache.mem)
	if unsafe.SliceData(c.code) != base {
		t.Fatal("serial compile retained a second code image")
	}
	if _, err := c.MarshalBinary(); err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !cache.sealed || unsafe.SliceData(cache.mem) != base {
		t.Fatal("instantiation must seal the existing image without copying")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := in.Invoke("f", I32(10))
	if err != nil || len(got) != 1 || got[0] != 11 {
		t.Fatalf("live instance after Compiled.Close: result=%v err=%v", got, err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if len(cache.mem) != 0 {
		t.Fatal("last instance did not release the mapped code")
	}
}

//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import (
	"strings"
	"testing"
)

// passiveDataModule moved to dataseg_shared_test.go

func TestPassiveDataMemoryInitAndDrop(t *testing.T) {
	c, err := Compile(passiveDataModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.boundsMode != BoundsChecksSignalsBased {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary: %v", err)
		}
		c, err = Load(blob)
		if err != nil {
			t.Fatalf("Load compiled blob: %v", err)
		}
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	if _, err := in.Invoke("init", I32(10), I32(1), I32(3)); err != nil {
		t.Fatalf("memory.init: %v", err)
	}
	if got := string(in.Memory().Bytes()[10:13]); got != "ell" {
		t.Fatalf("memory.init copied %q, want ell", got)
	}
	if _, err := in.Invoke("init", I32(20), I32(0), I32(5)); err != nil {
		t.Fatalf("second memory.init before drop: %v", err)
	}
	if got := string(in.Memory().Bytes()[20:25]); got != "hello" {
		t.Fatalf("second memory.init copied %q, want hello", got)
	}
	if _, err := in.Invoke("drop"); err != nil {
		t.Fatalf("data.drop: %v", err)
	}
	if _, err := in.Invoke("init", I32(30), I32(0), I32(1)); err == nil {
		t.Fatal("memory.init after data.drop succeeded; want trap")
	}
	if _, err := in.Invoke("init", I32(30), I32(0), I32(0)); err != nil {
		t.Fatalf("zero-length memory.init after data.drop: %v", err)
	}
}

func TestPassiveDataMemoryInitBoundsTrap(t *testing.T) {
	for _, tc := range []struct {
		name string
		dst  int32
		src  int32
		n    int32
	}{
		{name: "source", dst: 0, src: 4, n: 2},
		{name: "destination", dst: 65535, src: 0, n: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(MustCompile(passiveDataModule()), nil)
			if err != nil {
				t.Fatalf("Instantiate: %v", err)
			}
			defer in.Close()
			_, err = in.Invoke("init", I32(tc.dst), I32(tc.src), I32(tc.n))
			if err == nil {
				t.Fatal("memory.init succeeded; want trap")
			}
			if !strings.Contains(err.Error(), "trap") {
				t.Fatalf("memory.init error = %v, want trap", err)
			}
		})
	}
}

package wago

import (
	"errors"
	"fmt"
	"testing"
)

func TestNativeCodeLimitIsOneFinalModuleBudget(t *testing.T) {
	source := benchImportedModule(64, 8)
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(workers)
			baseline, err := Compile(cfg, source)
			if err != nil {
				t.Fatal(err)
			}
			size := uint64(baseline.CodeSize())
			baseline.Close()
			for _, limit := range []uint64{size + 1, size, size - 1, size / 2} {
				c, err := Compile(cfg.WithMaxNativeCodeBytes(limit), source)
				if limit >= size {
					if err != nil {
						t.Fatalf("limit %d: %v", limit, err)
					}
					if uint64(c.CodeSize()) != size {
						t.Fatalf("code size changed: %d", c.CodeSize())
					}
					c.Close()
				} else {
					if c != nil {
						c.Close()
						t.Fatal("failed compile published an artifact")
					}
					if !errors.Is(err, ErrResourceLimit) {
						t.Fatalf("limit %d: %v", limit, err)
					}
				}
			}
			// Failure cannot poison the compiler or transfer a partial native image.
			c, err := Compile(cfg.WithMaxNativeCodeBytes(size), source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": func(v int32) int32 { return v }}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			out, err := in.Invoke("f0", 34)
			if err != nil || len(out) != 1 || out[0] != 42 {
				t.Fatalf("compile after failure = %v, %v", out, err)
			}
		})
	}
}

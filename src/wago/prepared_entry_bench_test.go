package wago

import (
	"context"
	"testing"
)

func BenchmarkPreparedEntryModes(b *testing.B) {
	for _, mode := range []string{"direct", "scalar", "general", "shared", "runtime"} {
		b.Run(mode, func(b *testing.B) {
			c := benchMustCompile(b, benchAddOneModule())
			var in *Instance
			var err error
			if mode == "runtime" {
				rt := NewRuntime()
				defer rt.Close()
				mod, e := rt.Module(c)
				if e != nil {
					b.Fatal(e)
				}
				defer mod.Close()
				in, err = rt.Instantiate(context.Background(), mod)
			} else {
				in, err = Instantiate(c, InstantiateOptions{})
			}
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			fn, err := in.PrepareFunction("f")
			if err != nil {
				b.Fatal(err)
			}
			if mode == "scalar" || mode == "general" {
				fn.directIntFast = false
			}
			if mode == "general" {
				fn.scalarFast = false
			}
			if mode == "shared" {
				if _, err := in.ExportedFunc("f"); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := fn.Invoke1(41)
				if err != nil || len(out) != 1 || out[0] != 42 {
					b.Fatalf("call = %v, %v", out, err)
				}
			}
		})
	}
}

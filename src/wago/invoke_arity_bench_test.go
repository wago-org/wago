//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
)

// BenchmarkInvokeArity separates direct variadic syntax from expansion of
// reused argument storage for both by-name and resolved calls.
func BenchmarkInvokeArity(b *testing.B) {
	for _, arity := range []int{0, 1, 2, 4, 5} {
		b.Run(fmt.Sprintf("i32x%d", arity), func(b *testing.B) {
			compiled := benchMustCompile(b, hostToWasmI32SignatureModule(arity, 1))
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			fn, err := instance.WasmFunc("f")
			if err != nil {
				b.Fatal(err)
			}
			args := make([]uint64, arity)
			for i := range args {
				args[i] = I32(int32(i + 1))
			}
			b.Run("resolved/direct", func(b *testing.B) { benchmarkResolvedDirect(b, fn, arity) })
			b.Run("resolved/reused", func(b *testing.B) { benchmarkResolvedReused(b, fn, args) })
			b.Run("by-name/direct", func(b *testing.B) { benchmarkByNameDirect(b, instance, arity) })
			b.Run("by-name/reused", func(b *testing.B) { benchmarkByNameReused(b, instance, args) })
		})
	}
}

func benchmarkResolvedDirect(b *testing.B, fn *WasmFunc, arity int) {
	b.ReportAllocs()
	var out []uint64
	var err error
	b.ResetTimer()
	switch arity {
	case 0:
		for i := 0; i < b.N; i++ {
			out, err = fn.Invoke()
		}
	case 1:
		for i := 0; i < b.N; i++ {
			out, err = fn.Invoke(I32(1))
		}
	case 2:
		for i := 0; i < b.N; i++ {
			out, err = fn.Invoke(I32(1), I32(2))
		}
	case 4:
		for i := 0; i < b.N; i++ {
			out, err = fn.Invoke(I32(1), I32(2), I32(3), I32(4))
		}
	case 5:
		for i := 0; i < b.N; i++ {
			out, err = fn.Invoke(I32(1), I32(2), I32(3), I32(4), I32(5))
		}
	}
	b.StopTimer()
	if err != nil {
		b.Fatal(err)
	}
	benchResultSink = out
}

func benchmarkResolvedReused(b *testing.B, fn *WasmFunc, args []uint64) {
	b.ReportAllocs()
	var out []uint64
	var err error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err = fn.Invoke(args...)
	}
	b.StopTimer()
	if err != nil {
		b.Fatal(err)
	}
	benchResultSink = out
}

func benchmarkByNameDirect(b *testing.B, instance *Instance, arity int) {
	b.ReportAllocs()
	var out []uint64
	var err error
	b.ResetTimer()
	switch arity {
	case 0:
		for i := 0; i < b.N; i++ {
			out, err = instance.Invoke("f")
		}
	case 1:
		for i := 0; i < b.N; i++ {
			out, err = instance.Invoke("f", I32(1))
		}
	case 2:
		for i := 0; i < b.N; i++ {
			out, err = instance.Invoke("f", I32(1), I32(2))
		}
	case 4:
		for i := 0; i < b.N; i++ {
			out, err = instance.Invoke("f", I32(1), I32(2), I32(3), I32(4))
		}
	case 5:
		for i := 0; i < b.N; i++ {
			out, err = instance.Invoke("f", I32(1), I32(2), I32(3), I32(4), I32(5))
		}
	}
	b.StopTimer()
	if err != nil {
		b.Fatal(err)
	}
	benchResultSink = out
}

func benchmarkByNameReused(b *testing.B, instance *Instance, args []uint64) {
	b.ReportAllocs()
	var out []uint64
	var err error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err = instance.Invoke("f", args...)
	}
	b.StopTimer()
	if err != nil {
		b.Fatal(err)
	}
	benchResultSink = out
}

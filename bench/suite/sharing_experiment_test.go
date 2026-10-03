package wagobench

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	core "github.com/wago-org/wago/src/core/runtime"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

type sharingFixture struct {
	name  string
	bytes []byte
	want  uint64
}

func sharingU(v int) []byte {
	var b []byte
	for {
		x := byte(v & 127)
		v >>= 7
		if v != 0 {
			x |= 128
		}
		b = append(b, x)
		if v == 0 {
			return b
		}
	}
}
func sharingS(v int) []byte {
	var b []byte
	for {
		x := byte(v & 127)
		v >>= 7
		done := (v == 0 && x&64 == 0) || (v == -1 && x&64 != 0)
		if !done {
			x |= 128
		}
		b = append(b, x)
		if done {
			return b
		}
	}
}
func sharingModule(bodies [][]byte, locals int) []byte {
	b := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	sec := func(id byte, p []byte) { b = append(b, id); b = append(b, sharingU(len(p))...); b = append(b, p...) }
	sec(1, []byte{1, 96, 1, 127, 1, 127})
	p := sharingU(len(bodies))
	for range bodies {
		p = append(p, 0)
	}
	sec(3, p)
	sec(7, []byte{1, 3, 'r', 'u', 'n', 0, 0})
	p = sharingU(len(bodies))
	for _, body := range bodies {
		d := []byte{0}
		if locals > 0 {
			d = append([]byte{1}, sharingU(locals)...)
			d = append(d, 127)
		}
		d = append(d, body...)
		p = append(p, sharingU(len(d))...)
		p = append(p, d...)
	}
	sec(10, p)
	return b
}
func sharingFixtures() []sharingFixture {
	small := []byte{32, 0, 65, 1, 106, 11}
	pressure := []byte{}
	for k := 1; k <= 40; k++ {
		pressure = append(pressure, 32, 0, 65)
		pressure = append(pressure, sharingS(k)...)
		pressure = append(pressure, 106, 34, 1)
	}
	for k := 1; k < 40; k++ {
		pressure = append(pressure, 106)
	}
	pressure = append(pressure, 11)
	large := []byte{}
	for k := 0; k < 1000; k++ {
		large = append(large, 32, 0, 65, 1, 106, 33, 0)
	}
	large = append(large, 32, 0, 11)
	deep := []byte{}
	for k := 0; k < 24; k++ {
		deep = append(deep, 2, 127)
	}
	deep = append(deep, 32, 0)
	for k := 0; k < 25; k++ {
		deep = append(deep, 11)
	}
	manylocals := []byte{}
	for k := 1; k <= 192; k++ {
		manylocals = append(manylocals, 32, 0, 33)
		manylocals = append(manylocals, sharingU(k)...)
	}
	manylocals = append(manylocals, 32)
	manylocals = append(manylocals, sharingU(192)...)
	manylocals = append(manylocals, 11)
	join := []byte{32, 0, 4, 127, 65, 7, 34, 1, 5, 65, 9, 34, 1, 11, 32, 1, 106, 11}
	fallback := []byte{32, 0, 65, 2, 109, 11}
	many := make([][]byte, 512)
	for k := range many {
		many[k] = small
	}
	mixed := append([][]byte{large}, many...)
	return []sharingFixture{{"small", sharingModule([][]byte{small}, 1), 4}, {"pressure", sharingModule([][]byte{pressure}, 1), 940}, {"join", sharingModule([][]byte{join}, 1), 14}, {"large", sharingModule([][]byte{large}, 1), 1003}, {"deep", sharingModule([][]byte{deep}, 1), 3}, {"locals", sharingModule([][]byte{manylocals}, 192), 3}, {"many", sharingModule(many, 1), 4}, {"large_then_small", sharingModule(mixed, 1), 1003}, {"fallback", sharingModule([][]byte{fallback}, 1), 1}}
}
func TestSharingFixtures(t *testing.T) {
	for _, f := range sharingFixtures() {
		t.Run(f.name, func(t *testing.T) {
			c, e := wago.Compile(wago.NewRuntimeConfig().WithFunctionWorkers(1), f.bytes)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			in, e := wago.Instantiate(c)
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			got, e := in.Invoke("run", 3)
			if e != nil || len(got) != 1 || got[0] != f.want {
				t.Fatalf("got %v %v want %d", got, e, f.want)
			}
			m, _ := wasm.DecodeModule(f.bytes)
			t.Logf("fixture sha256=%x functions=%d module_bytes=%d", sha256.Sum256(f.bytes), len(m.Code), len(f.bytes))
		})
	}
}
func BenchmarkSharingNative(b *testing.B) {
	for _, f := range sharingFixtures() {
		b.Run(f.name, func(b *testing.B) {
			m, e := wasm.DecodeModule(f.bytes)
			if e != nil {
				b.Fatal(e)
			}
			if e = wasm.ValidateModule(m); e != nil {
				b.Fatal(e)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, e := benchCompileModuleWorkers(m, 1)
				if e != nil {
					b.Fatal(e)
				}
				if e = c.Close(); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
func BenchmarkSharingFull(b *testing.B) {
	for _, f := range sharingFixtures() {
		b.Run(f.name, func(b *testing.B) {
			cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, e := wago.Compile(cfg, f.bytes)
				if e != nil {
					b.Fatal(e)
				}
				if e = c.Close(); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
func BenchmarkSharingExec(b *testing.B) {
	for _, f := range sharingFixtures() {
		b.Run(f.name, func(b *testing.B) {
			c, e := wago.Compile(wago.NewRuntimeConfig().WithFunctionWorkers(1), f.bytes)
			if e != nil {
				b.Fatal(e)
			}
			defer c.Close()
			in, e := wago.Instantiate(c)
			if e != nil {
				b.Fatal(e)
			}
			defer in.Close()
			got, e := in.Invoke("run", 3)
			if e != nil || len(got) != 1 || got[0] != f.want {
				b.Fatal(got, e)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, e = in.Invoke("run", 3)
				if e != nil || len(got) != 1 || got[0] != f.want {
					b.Fatal(got, e)
				}
			}
		})
	}
}
func sharingSnapshot(t *testing.T, phase string, outputs []*wago.Module) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	var u syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &u)
	rss, _ := os.ReadFile("/proc/self/statm")
	code := 0
	for _, c := range outputs {
		code += c.Compiled().CodeSize()
	}
	rssFields := strings.Fields(string(rss))
	rssBytes := uint64(0)
	if len(rssFields) > 1 {
		pages, _ := strconv.ParseUint(rssFields[1], 10, 64)
		rssBytes = pages * uint64(os.Getpagesize())
	}
	data, _ := json.Marshal(map[string]any{"rss_bytes": rssBytes, "phase": phase, "modules": len(outputs), "code_bytes": code, "heap_alloc": m.HeapAlloc, "heap_inuse": m.HeapInuse, "heap_objects": m.HeapObjects, "heap_released": m.HeapReleased, "total_alloc": m.TotalAlloc, "mallocs": m.Mallocs, "statm": string(rss), "peak_rss_kib": u.Maxrss, "native": core.ProcessNativeMemoryStats()})
	t.Log(string(data))
	if dir := os.Getenv("WAGO_SHARING_PROFILE_DIR"); dir != "" {
		f, e := os.Create(filepath.Join(dir, phase+".heap"))
		if e != nil {
			t.Fatal(e)
		}
		if e = pprof.WriteHeapProfile(f); e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	runtime.KeepAlive(outputs)
}
func TestSharingMemory(t *testing.T) {
	if os.Getenv("WAGO_SHARING_MEMORY") != "1" {
		t.Skip("diagnostic")
	}
	fs := sharingFixtures()
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	sharingSnapshot(t, "initial", nil)
	for i := 0; i < 30; i++ {
		for _, f := range fs {
			c, e := rt.Compile(f.bytes)
			if e != nil {
				t.Fatal(e)
			}
			if e = c.Close(); e != nil {
				t.Fatal(e)
			}
		}
	}
	sharingSnapshot(t, "compile_release_270", nil)
	for cycle := 0; cycle < 3; cycle++ {
		var out []*wago.Module
		for i := 0; i < 4; i++ {
			for _, f := range fs {
				c, e := rt.Compile(f.bytes)
				if e != nil {
					t.Fatal(e)
				}
				out = append(out, c)
			}
		}
		sharingSnapshot(t, fmt.Sprintf("retained_%d", cycle), out)
		for _, c := range out {
			if e := c.Close(); e != nil {
				t.Fatal(e)
			}
		}
		out = nil
		sharingSnapshot(t, fmt.Sprintf("released_%d", cycle), nil)
	}
	runtime.KeepAlive(rt)
	if e := rt.CloseContext(context.Background()); e != nil {
		t.Fatal(e)
	}
	rt = nil
	sharingSnapshot(t, "runtime_released", nil)
	runtime.KeepAlive(fs)
	runtime.KeepAlive(cfg)
}

// Native mapping retention is measured separately from public Runtime compile,
// which deliberately defers executable mappings until an instance needs code.
func TestSharingMappedMemory(t *testing.T) {
	if os.Getenv("WAGO_SHARING_MEMORY") != "1" {
		t.Skip("diagnostic")
	}
	fs := sharingFixtures()
	mods := make([]*wasm.Module, len(fs))
	functions := 0
	for i, f := range fs {
		m, e := wasm.DecodeModule(f.bytes)
		if e != nil {
			t.Fatal(e)
		}
		if e = wasm.ValidateModule(m); e != nil {
			t.Fatal(e)
		}
		mods[i] = m
		functions += len(m.Code)
	}
	snapshot := func(phase string, out []*benchCompiledModule) {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		var u syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &u)
		code, mapped := 0, 0
		for _, c := range out {
			code += len(c.Code)
			mapped += (len(c.Code) + 4095) &^ 4095
		}
		rss, _ := os.ReadFile("/proc/self/statm")
		parts := strings.Fields(string(rss))
		pages := uint64(0)
		if len(parts) > 1 {
			pages, _ = strconv.ParseUint(parts[1], 10, 64)
		}
		d, _ := json.Marshal(map[string]any{"phase": phase, "modules": len(out), "functions_per_corpus": functions, "code_bytes": code, "mapped_bytes_page_accounting": mapped, "heap_alloc": m.HeapAlloc, "heap_inuse": m.HeapInuse, "heap_objects": m.HeapObjects, "heap_released": m.HeapReleased, "total_alloc": m.TotalAlloc, "mallocs": m.Mallocs, "rss_bytes": pages * uint64(os.Getpagesize()), "peak_rss_kib": u.Maxrss})
		t.Log(string(d))
		runtime.KeepAlive(out)
	}
	snapshot("initial", nil)
	for i := 0; i < 30; i++ {
		for _, m := range mods {
			c, e := benchCompileModuleWorkers(m, 1)
			if e != nil {
				t.Fatal(e)
			}
			if e = c.Close(); e != nil {
				t.Fatal(e)
			}
		}
	}
	snapshot("compile_release_270", nil)
	for cycle := 0; cycle < 3; cycle++ {
		var out []*benchCompiledModule
		for i := 0; i < 4; i++ {
			for _, m := range mods {
				c, e := benchCompileModuleWorkers(m, 1)
				if e != nil {
					t.Fatal(e)
				}
				out = append(out, c)
			}
		}
		snapshot(fmt.Sprintf("retained_%d", cycle), out)
		for _, c := range out {
			if e := c.Close(); e != nil {
				t.Fatal(e)
			}
		}
		out = nil
		snapshot(fmt.Sprintf("released_%d", cycle), nil)
	}
	runtime.KeepAlive(mods)
	runtime.KeepAlive(fs)
}

//go:build linux && amd64 && wago_profile

// Command adapterunwind qualifies compacted Railshot adapters with perf/libdw.
// CompactNative is a backend rollout option, so this fixture uses the compiler
// and production runtime directly rather than changing the public runtime API.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/profile"
	compiler "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func save(dir, name string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(dir, name), data, 0600))
}

func module(count int) []byte {
	var funcs, exports, bodies [][]byte
	for i := 0; i < count; i++ {
		// Count down in the body, then return two independently checked values.
		// The identical signatures permit sharing the adapter around each body.
		body := []byte{1, 1, 0x7f, 0x41}
		body = append(body, wasmtest.SLEB32(100000)...)
		body = append(body, 0x21, 0, 0x03, 0x40, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i+1))...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i+11))...)
		body = append(body, 0x0b)
		funcs = append(funcs, wasmtest.ULEB(0))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("loop%d", i), 0, uint32(i)))
		bodies = append(bodies, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func main() {
	if len(os.Args) != 4 || (os.Args[2] != "legacy" && os.Args[2] != "delta" && os.Args[2] != "tail") || (os.Args[3] != "maps" && os.Args[3] != "none") {
		panic("usage: adapterunwind NEW_DIRECTORY legacy|delta|tail maps|none")
	}
	dir, mode, maps := os.Args[1], os.Args[2], os.Args[3] == "maps"
	must(os.Mkdir(dir, 0700))
	count := 6
	if mode == "legacy" {
		count = 5 // Profitable sharing, below the six-adapter target-delta threshold.
	}
	wasmBytes := module(count)
	m, err := wasm.DecodeModule(wasmBytes)
	must(err)
	var stats compiler.ModuleStats
	cm, err := compiler.CompileModuleWith(m, compiler.CompileOptions{
		CompactNative: true, DeferCodeMapping: true, Profile: true,
		UnwindMaps: maps, Stats: &stats,
		Optimizations: map[string]bool{"reg-abi": true, "inline": false, "shared-adapters": mode != "tail"},
	})
	must(err)
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	shared := false
	for _, region := range stats.ProfileRegions {
		shared = shared || region.Kind == "shared-adapter"
	}
	if !shared || (len(stats.UnwindRanges) > 0) != maps {
		panic(fmt.Sprintf("requested adapter layout or unwind metadata was not generated: shared=%v rows=%d", shared, len(stats.UnwindRanges)))
	}
	for i, fn := range stats.Funcs {
		if mode == "delta" && (fn.NativeSize.HostAdapterBytes != 10 || cm.Code[cm.Entry[i]] != 0x68) {
			panic("expected PUSH/JMP target-delta thunk")
		}
		if mode == "legacy" && (fn.NativeSize.HostAdapterBytes != 12 || cm.Code[cm.Entry[i]] != 0x48) {
			panic("expected LEA/JMP legacy thunk")
		}
	}
	eng, err := runtime.NewEngine()
	must(err)
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	must(err)
	defer jm.Close()
	arena, err := runtime.NewArena(4096)
	must(err)
	defer arena.Close()
	args, results, trap := arena.Alloc(256), arena.Alloc(256), arena.Alloc(runtime.TrapBufferBytes)
	memory, base, err := runtime.MapCode(cm.Code)
	must(err)
	defer func() {
		if memory != nil {
			must(runtime.Unmap(memory))
		}
	}()
	session := jitprofile.New(jitprofile.Options{IncludeCode: true, UnwindMaps: maps})
	defer session.Close()
	moduleID := fmt.Sprintf("%x", sha256.Sum256(wasmBytes))
	artifactID := fmt.Sprintf("%x", sha256.Sum256(cm.Code))
	id := session.Register(jitprofile.Image{
		ModuleID: moduleID, ArtifactID: artifactID, Base: uint64(base), Size: uint64(len(cm.Code)),
		Target: "linux/amd64", Regions: stats.ProfileRegions, Unwind: stats.UnwindRanges,
		UnwindCoverage: "amd64-fixed-frames-and-adapters",
	}, cm.Code)
	if id == 0 {
		panic("image registration failed")
	}
	dump, err := profile.OpenJITDump(dir)
	must(err)
	defer dump.Close()
	events, status := session.Read(0)
	if status.Dropped != 0 || len(events) != 1 {
		panic("invalid publication history")
	}
	must(dump.Write(events))
	start := time.Now()
	iterations := 0
	for time.Since(start) < 2*time.Second {
		i := iterations % count
		must(eng.Call(base+uintptr(cm.Entry[i]), args, jm.LinearMemory(), trap, results))
		if binary.LittleEndian.Uint64(results) != uint64(i+1) || binary.LittleEndian.Uint64(results[8:]) != uint64(i+11) {
			panic("incorrect compact-adapter result")
		}
		iterations++
	}
	elapsed := time.Since(start)
	must(runtime.Unmap(memory))
	memory = nil
	session.Retire(id)
	events, status = session.Read(0)
	if status.Dropped != 0 || len(events) != 2 || events[1].Kind != "retire" {
		panic("invalid retirement history")
	}
	must(dump.Write(events[1:]))
	must(dump.Close())
	save(dir, "images.json", events)
	save(dir, "fixture.json", map[string]any{
		"kind": "Railshot compact adapters", "mode": mode, "unwind_maps": maps,
		"functions": count, "iterations": iterations, "elapsed_ns": elapsed.Nanoseconds(), "metadata_status": status,
	})
	fmt.Printf("validated %d invocations through %s adapters; requested unwind maps=%v\n", iterations, mode, maps)
}

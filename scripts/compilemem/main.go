//go:build linux

// compilemem measures one cold public compilation in a fresh Linux process.
// Run an identical binary/command per revision. MaxRSS is the kernel's exact
// process high-water mark (including startup and input); allocation traffic and
// post-GC retained heap are separate metrics, never substitutes for peak RSS.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	wago "github.com/wago-org/wago"
)

func rss() int64 {
	var r syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r); err != nil {
		panic(err)
	}
	return r.Maxrss * 1024
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: compilemem module.wasm")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	runtime.GC()
	var before, after, retained, closed runtime.MemStats
	runtime.ReadMemStats(&before)
	startupRSS := rss()
	cm, err := wago.Compile(nil, data)
	if err != nil {
		panic(err)
	}
	runtime.ReadMemStats(&after)
	peakRSS := rss() // record before diagnostic GCs / JSON allocation
	codeBytes := cm.CodeSize()
	runtime.GC()
	runtime.ReadMemStats(&retained)
	runtime.KeepAlive(cm)
	activePayloadBytes := 0
	for _, segment := range cm.Data {
		activePayloadBytes += len(segment.Bytes)
	}
	activeSegments := len(cm.Data)
	activeDescriptorBytes := uintptr(activeSegments) * unsafe.Sizeof(wago.DataInit{})
	codeHash := sha256.New()
	if _, err := cm.WriteCodeTo(codeHash); err != nil {
		panic(err)
	}
	var digest [sha256.Size]byte
	codeHash.Sum(digest[:0])
	codeHash = nil
	if err = cm.Close(); err != nil {
		panic(err)
	}
	cm = nil
	runtime.GC()
	runtime.ReadMemStats(&closed)
	runtime.KeepAlive(data)
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"input": os.Args[1], "input_bytes": len(data), "go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0),
		"startup_peak_rss_bytes": startupRSS, "compile_peak_rss_bytes": peakRSS,
		"allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs,
		"compile_gc_cycles": after.NumGC - before.NumGC, "heap_before_bytes": before.HeapAlloc,
		"heap_after_compile_bytes": after.HeapAlloc, "heap_retained_result_bytes": retained.HeapAlloc,
		"active_data_segments": activeSegments, "active_data_payload_bytes": activePayloadBytes,
		"active_data_public_descriptor_bytes": activeDescriptorBytes,
		"input_sha256":                        fmt.Sprintf("%x", sha256.Sum256(data)),
		"heap_after_close_bytes":              closed.HeapAlloc, "native_code_bytes": codeBytes, "native_code_sha256": fmt.Sprintf("%x", digest),
	}); err != nil {
		panic(err)
	}
}

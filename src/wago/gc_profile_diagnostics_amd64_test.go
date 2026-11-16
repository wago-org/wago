//go:build linux && amd64 && wago_profile && wago_gc_direct_trusted_alloc

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestGCNativePerfPermission(t *testing.T) {
	out := os.Getenv("GC_NATIVE_PERMISSION_OUT")
	if out == "" {
		t.Skip("set GC_NATIVE_PERMISSION_OUT")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	attr := unix.PerfEventAttr{Type: unix.PERF_TYPE_SOFTWARE, Size: uint32(unsafe.Sizeof(unix.PerfEventAttr{})), Config: unix.PERF_COUNT_SW_CPU_CLOCK, Bits: unix.PerfBitDisabled | unix.PerfBitExcludeKernel | unix.PerfBitExcludeHv}
	fd, err := unix.PerfEventOpen(&attr, 0, -1, -1, unix.PERF_FLAG_FD_CLOEXEC)
	r := map[string]any{"operation": "disabled self-thread cpu-clock open/close", "enabled": false, "sampling": false, "mmap": false, "exclude_kernel": true, "exclude_hypervisor": true, "success": err == nil}
	if err != nil {
		r["error"] = err.Error()
		if errno, ok := err.(unix.Errno); ok {
			r["errno"] = int(errno)
		}
	} else {
		if err = unix.Close(fd); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := json.MarshalIndent(r, "", "  ")
	if err = os.WriteFile(out, data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
}

func TestGCNativeCopyProfileDiagnostics(t *testing.T) {
	out := os.Getenv("GC_NATIVE_COPY_OUT")
	if out == "" {
		t.Skip("set GC_NATIVE_COPY_OUT")
	}
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"array-copy-checksum", "array-loop-copy-checksum"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../bench/experiments/gc-v8", "gc-"+name+".wasm"))
			if err != nil {
				t.Fatal(err)
			}
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false)
			plain, err := Compile(cfg, raw)
			if err != nil {
				t.Fatal(err)
			}
			defer plain.Close()
			write := func(file string, data []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(out, name+"-"+file), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("plain.bin", plain.code)
			for _, sources := range []bool{false, true} {
				label := "functions"
				if sources {
					label = "sources"
				}
				session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: sources})
				defer session.Close()
				rt := NewRuntime(WithRuntimeConfig(cfg.WithCodeProfile(session)))
				defer rt.Close()
				mod, err := rt.Compile(raw)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				config := GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 2 << 20, ThroughputHeapBytes: 8 << 20}
				in, err := rt.Instantiate(context.Background(), mod, WithGC(config))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if in.gcDirectView == nil || !bytes.Equal(in.c.codeCache.mem[:len(mod.c.code)], mod.c.code) {
					t.Fatal("loaded code mismatch")
				}
				events, status := session.Read(0)
				if status.Dropped != 0 {
					t.Fatal(status)
				}
				var diff []int
				for i := 0; i < min(len(plain.code), len(mod.c.code)); i++ {
					if plain.code[i] != mod.c.code[i] {
						diff = append(diff, i)
					}
				}
				write(label+".bin", mod.c.code)
				receipt := map[string]any{"case": name, "source_maps": sources, "exact_parity": bytes.Equal(plain.code, mod.c.code), "plain_bytes": len(plain.code), "profile_bytes": len(mod.c.code), "plain_sha256": fmt.Sprintf("%x", sha256.Sum256(plain.code)), "profile_sha256": fmt.Sprintf("%x", sha256.Sum256(mod.c.code)), "different_offsets_common_extent": diff, "events": events, "status": status, "config": config, "guest_invocations": 0, "samples": false}
				b, _ := json.MarshalIndent(receipt, "", "  ")
				write(label+".json", b)
				t.Logf("COPY_PROFILE_DIAGNOSTIC case=%s mode=%s parity=%v plain=%d observed=%d", name, label, bytes.Equal(plain.code, mod.c.code), len(plain.code), len(mod.c.code))
				if !sources && !bytes.Equal(plain.code, mod.c.code) {
					t.Error("function-only profiling changed bytes")
				}
			}
		})
	}
}

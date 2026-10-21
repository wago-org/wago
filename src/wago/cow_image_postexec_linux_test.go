//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The real PHP data segments are kept in their original order. The added Wasm
// function reads one byte per initial memory page and, in write mode, changes
// that byte. This isolates read sharing versus first-write CoW without a PHP
// interpreter heap or a second production payload representation.
func cowPHPPageExerciseModule(tb testing.TB) ([]byte, []byte, uint32) {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
	if err != nil {
		tb.Fatal(err)
	}
	original, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
	if err != nil {
		tb.Fatal(err)
	}
	defer original.Close()
	initialBytes := int(original.MemMinPages) * 65536
	if initialBytes < 4096 || initialBytes > 1<<28 {
		tb.Fatalf("unexpected PHP initial memory: %d", initialBytes)
	}
	initial := make([]byte, initialBytes)
	segments := make([][]byte, original.activeDataCount())
	for i := range segments {
		d := original.activeDataAt(i)
		if d.MemoryIndex != 0 || d.Offset.HasGlobal || len(d.Offset.Expr) != 0 {
			tb.Fatalf("PHP segment %d left constant-offset class", i)
		}
		if int(d.Offset.Base)+len(d.Bytes) > len(initial) {
			tb.Fatalf("PHP segment %d exceeds initial memory", i)
		}
		copy(initial[d.Offset.Base:], d.Bytes)
		seg := []byte{0, 0x41}
		seg = append(seg, wasmtest.SLEB32(int32(d.Offset.Base))...)
		seg = append(seg, 0x0b)
		seg = append(seg, wasmtest.ULEB(uint32(len(d.Bytes)))...)
		segments[i] = append(seg, d.Bytes...)
	}
	pageCount := uint32((initialBytes + 4095) / 4096)
	// One group of two i32 locals follows the single i32 mode parameter.
	body := []byte{1, 2, 0x7f, 0x41, 0, 0x21, 1, 0x41, 0, 0x21, 2,
		0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x41}
	body = append(body, wasmtest.SLEB32(int32(initialBytes))...)
	body = append(body, 0x4f, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x2d, 0, 0, 0x6a, 0x21, 2,
		0x20, 0, 0x04, 0x40,
		0x20, 1, 0x20, 1, 0x2d, 0, 0, 0x41, 1, 0x6a, 0x3a, 0, 0,
		0x0b, 0x20, 1, 0x41)
	body = append(body, wasmtest.SLEB32(4096)...)
	body = append(body, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	mem := append([]byte{0}, wasmtest.ULEB(original.MemMinPages)...)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec(mem)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("exercise", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
		wasmtest.Section(11, wasmtest.Vec(segments...)),
	)
	return module, initial, pageCount
}

type cowPageSnapshot struct {
	PSSKB, RSSKB, DirtyKB int
}

func cowPageProcessSnapshot() (cowPageSnapshot, error) {
	raw, err := os.ReadFile("/proc/self/smaps_rollup")
	if err != nil {
		return cowPageSnapshot{}, err
	}
	var out cowPageSnapshot
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch fields[0] {
		case "Pss:":
			out.PSSKB = value
		case "Rss:":
			out.RSSKB = value
		case "Private_Dirty:":
			out.DirtyKB = value
		}
	}
	if out.PSSKB == 0 || out.RSSKB == 0 {
		return out, fmt.Errorf("incomplete smaps_rollup: %+v", out)
	}
	return out, nil
}

type cowPageReport struct {
	Mode, Exercise, StateSHA256              string
	Eager                                    bool
	Instances, Pages, NativeBytes            int
	CompileMS, InstantiateMS, ExecuteMS      float64
	Compiled, Initialized, Executed, ReadAll cowPageSnapshot
}

func TestCOWImagePageReadWriteMemory(t *testing.T) {
	mode := os.Getenv("WAGO_924_POSTEXEC_MODE")
	exercise := os.Getenv("WAGO_924_PAGE_EXERCISE")
	if mode == "" || exercise == "" {
		t.Skip("opt-in separate-process page sharing experiment")
	}
	if (mode != "baseline" && mode != "cow") || (exercise != "read" && exercise != "write") {
		t.Fatalf("invalid mode/exercise %q/%q", mode, exercise)
	}
	count, err := strconv.Atoi(os.Getenv("WAGO_924_POSTEXEC_INSTANCES"))
	if err != nil || count < 1 || count > 100 {
		t.Fatalf("instances must be 1..100: %v", err)
	}
	flag := "0"
	if mode == "cow" {
		flag = "1"
		if os.Getenv("WAGO_924_EAGER") == "1" {
			flag = "eager"
		}
	}
	t.Setenv("WAGO_EXPERIMENT_COW_IMAGE", flag)
	module, initial, pages := cowPHPPageExerciseModule(t)
	var want uint32
	for i := 0; i < len(initial); i += 4096 {
		want += uint32(initial[i])
	}
	if exercise == "write" {
		for i := 0; i < len(initial); i += 4096 {
			initial[i]++
		}
	}
	wantState := sha256.Sum256(initial)
	report := cowPageReport{Mode: mode, Exercise: exercise, Instances: count, Pages: int(pages), Eager: flag == "eager"}
	started := time.Now()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	report.CompileMS = float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	report.NativeBytes = c.CodeSize()
	// Drop the original PHP compiler's transient heap from fixture construction
	// before taking the first process snapshot in each independent process.
	runtime.GC()
	debug.FreeOSMemory()
	if report.Compiled, err = cowPageProcessSnapshot(); err != nil {
		t.Fatal(err)
	}
	instances := make([]*Instance, 0, count)
	defer func() {
		for _, in := range instances {
			_ = in.Close()
		}
	}()
	for i := 0; i < count; i++ {
		started = time.Now()
		in, err := Instantiate(c)
		report.InstantiateMS += float64(time.Since(started).Microseconds()) / 1000
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, in)
	}
	if report.Initialized, err = cowPageProcessSnapshot(); err != nil {
		t.Fatal(err)
	}
	arg := I32(0)
	if exercise == "write" {
		arg = I32(1)
	}
	for i, in := range instances {
		started = time.Now()
		got, err := in.Invoke("exercise", arg)
		report.ExecuteMS += float64(time.Since(started).Microseconds()) / 1000
		if err != nil || len(got) != 1 || uint32(AsI32(got[0])) != want {
			t.Fatalf("instance %d checksum = %v, %v; want %d", i, got, err, want)
		}
	}
	if report.Executed, err = cowPageProcessSnapshot(); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for i, in := range instances {
		memory := in.Memory().UnsafeBytes()
		if got := sha256.Sum256(memory); got != wantState {
			t.Fatalf("instance %d state mismatch: %x versus %x", i, got, wantState)
		}
		_, _ = h.Write(memory)
	}
	report.StateSHA256 = hex.EncodeToString(h.Sum(nil))
	if report.ReadAll, err = cowPageProcessSnapshot(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("COW_PAGES %s", encoded)
}

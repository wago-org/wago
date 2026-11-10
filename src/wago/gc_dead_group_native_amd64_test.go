//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"strings"
	"testing"
	"unsafe"
)

// gcDeadGroupNativeWord keeps Go pointer provenance for inline collector fields
// exposed as native addresses. The instance owns the collector until Close.
func gcDeadGroupNativeWord(t *testing.T, collector *gc.Collector, address, offset uintptr) *uint32 {
	t.Helper()
	base, size := uintptr(unsafe.Pointer(collector)), unsafe.Sizeof(*collector)
	if address < base {
		t.Fatal("native field address precedes collector")
	}
	field := address - base
	if field > size || offset > size-field || unsafe.Sizeof(uint32(0)) > size-field-offset {
		t.Fatal("native field extends beyond collector")
	}
	return (*uint32)(unsafe.Add(unsafe.Pointer(collector), field+offset))
}

func TestGCDeadDefaultGroupNativeBoundaries(t *testing.T) {
	for _, n := range []int{2, 3, 4} {
		for _, kind := range []string{"room", "exact", "short", "mixed-room", "mixed-exact", "mixed-short"} {
			form := "plain"
			if strings.HasPrefix(kind, "mixed-") {
				form = "mixed"
			}
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(n, 1, form))
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("run", 1); err != nil || len(got) != 1 || got[0] != 73 {
				t.Fatalf("warm%v,%v", got, err)
			}
			view := in.gc.NativeView()
			bump := gcDeadGroupNativeWord(t, in.gc, view.NurseryBump, 0)
			cursor := gcDeadGroupNativeWord(t, in.gc, view.StructAllocState, gc.NativeStructAllocCursorOffset)
			count := gcDeadGroupNativeWord(t, in.gc, view.StructAllocState, gc.NativeStructAllocCountOffset)
			if *count == 0 {
				t.Fatal("warmup did not prepare native batch")
			}
			before := in.gc.Stats()
			generation, oldCursor := view.RefreshGeneration, *cursor
			end := *bump
			for i := range n {
				end = (end + 7) &^ 7
				if form == "mixed" && i%2 == 1 {
					end += 48
				} else {
					end += 24
				}
			}
			savedLimit := view.NurseryAllocBytes
			switch kind {
			case "exact", "mixed-exact":
				view.NurseryAllocBytes = end
			case "short", "mixed-short":
				view.NurseryAllocBytes = end - 1
			}
			_, err = in.Invoke("leaf")
			// Restore the view even after a failed assertion; Close retains ownership.
			view.NurseryAllocBytes = savedLimit
			if err != nil {
				t.Fatalf("n%d kind%s:%v", n, kind, err)
			}
			if got := in.gc.Stats().Allocations - before.Allocations; got != uint64(n) {
				t.Fatalf("n%d kind%s charges%d", n, kind, got)
			}
			if *bump != end {
				t.Fatalf("n%d kind%s bump%d want%d; a miss must not charge a prefix", n, kind, *bump, end)
			}
			if strings.HasSuffix(kind, "short") {
				if view.RefreshGeneration <= generation {
					t.Fatal("short group did not take checked calls")
				}
			} else {
				if view.RefreshGeneration != generation || *cursor != oldCursor {
					t.Fatal("fitting group consumed handles or called Go")
				}
			}
			in.Close()
			c.Close()
		}
	}
}

func TestGCDeadDefaultGroupSingleReservation(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDeadGroupModule(4, 1, "only"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("leaf"); err != nil {
		t.Fatal(err)
	}
	view := in.gc.NativeView()
	count := gcDeadGroupNativeWord(t, in.gc, view.StructAllocState, gc.NativeStructAllocCountOffset)
	if *count != 1 {
		t.Fatalf("dead helper reserved%d handles want1", *count)
	}
	// Switch the single proof identity to an ordinary array reservation.
	if !in.gc.PrepareNativeAllocation(80) {
		t.Fatal("array reservation rejected single dead handle")
	}
	if *count != 32 {
		t.Fatalf("array count%d want32", *count)
	}
	end := gcDeadGroupNativeWord(t, in.gc, view.StructAllocState, gc.NativeStructAllocChunkEndOffset)
	if *end == 0 {
		t.Fatal("array transition did not reserve a chunk")
	}
	bump := gcDeadGroupNativeWord(t, in.gc, view.NurseryBump, 0)
	beforeBump, generation := *bump, view.RefreshGeneration
	before := in.gc.Stats().Allocations
	if _, err := in.Invoke("leaf"); err != nil {
		t.Fatal(err)
	}
	if view.RefreshGeneration != generation || *bump != beforeBump+96 || in.gc.Stats().Allocations-before != 4 {
		t.Fatal("dead group did not charge beyond the array chunk in native code")
	}
	chargedEnd := *bump
	in.gc.CancelNativeAllocationBatch()
	if *count != 0 || *bump != chargedEnd {
		t.Fatal("array cancellation rewound later dead charges")
	}
}

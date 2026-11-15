package gc

import (
	"encoding/binary"
	"testing"
)

func TestNativeReferenceWordMintAndFallback(t *testing.T) {
	for _, profile := range []Profile{ProfileThroughput, ProfileTiny} {
		c, err := NewCollector(Config{Profile: profile}, typedRefDescs(t))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for _, id := range []TypeID{0, 1, 2, 5} {
			r, err := c.NewStructDefaultWithRoots(id, EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			checked, err := c.TypedReference(r)
			if err != nil {
				t.Fatal(err)
			}
			if got := c.TryNativeReferenceWord(r); got != checked.NativeWord() {
				t.Fatalf("profile%d type%d: native word%#x want%#x", profile, id, got, checked.NativeWord())
			}
			if allocs := testing.AllocsPerRun(100, func() { c.TryNativeReferenceWord(r) }); allocs != 0 {
				t.Fatalf("native mint allocated %g times", allocs)
			}
		}
		for _, r := range []Ref{Null(), I31New(-1), Ref(0xfffffffe)} {
			if got := c.TryNativeReferenceWord(r); got != uint64(r) {
				t.Fatalf("native mint changed fallback reference%#x to%#x", r, got)
			}
		}
		r, err := c.NewStructDefaultWithRoots(0, EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		object := c.bytes(r)
		binary.LittleEndian.PutUint32(object, 0xffffffff)
		if got := c.TryNativeReferenceWord(r); got != uint64(r) {
			t.Fatalf("unknown header minted type evidence: %#x", got)
		}
		if allocs := testing.AllocsPerRun(100, func() { c.TryNativeReferenceWord(r) }); allocs != 0 {
			t.Fatalf("invalid header mint allocated %g times", allocs)
		}
		binary.LittleEndian.PutUint32(object, 0)
		entry := c.handles[handleOf(r)]
		c.handles[handleOf(r)].size = HeaderSize - 1
		if got := c.TryNativeReferenceWord(r); got != uint64(r) {
			t.Fatalf("short object minted type evidence: %#x", got)
		}
		c.handles[handleOf(r)] = entry
		view := c.NativeView()
		for _, field := range []string{"handle-count", "handles", "space-base", "space-bytes"} {
			handles, count, space := view.Handles, view.HandleCount, view.Spaces[entry.space]
			switch field {
			case "handle-count":
				view.HandleCount = 0
			case "handles":
				view.Handles = 0
			case "space-base":
				view.Spaces[entry.space].Base = 0
			case "space-bytes":
				view.Spaces[entry.space].Bytes = 0
			}
			if got := c.TryNativeReferenceWord(r); got != uint64(r) {
				t.Fatalf("%s: mismatched native view minted type evidence %#x", field, got)
			}
			var result uint64
			if allocs := testing.AllocsPerRun(100, func() { result = c.TryNativeReferenceWord(r) }); allocs != 0 || result != uint64(r) {
				t.Fatalf("%s: fallback allocated %g times or changed the reference", field, allocs)
			}
			view.Handles, view.HandleCount, view.Spaces[entry.space] = handles, count, space
		}
	}
}

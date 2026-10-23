//go:build wago_profile && linux && (amd64 || arm64) && !tinygo

package wago

import (
	"runtime"
	"testing"
)

func TestGCNonrootElemDropHelperEntries(t *testing.T) {
	const iterations = 32
	for mode, name := range []string{"nonzero", "zero", "mixed"} {
		t.Run(name, func(t *testing.T) {
			trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
			defer trace.Close()
			c, err := Compile(gcElemDropConfig().WithCodeProfile(trace), gcElemDropModule(false))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("loop", iterations, uint64(mode)); err != nil || len(got) != 1 || got[0] != iterations {
				t.Fatalf("loop:%v,%v", got, err)
			}
			count := 0
			for _, span := range trace.Spans() {
				if span.Kind == "gc-helper" && span.Function == int(gcArrayDropElem) {
					count++
				}
			}
			t.Logf("elem.drop helper entries=%d operations=%d", count, iterations)
			want := 0
			if runtime.GOARCH == "amd64" {
				if mode == 1 {
					want = iterations
				}
				if mode == 2 {
					want = iterations / 2
				}
			}
			if count != want {
				t.Fatalf("helper entries=%d want%d", count, want)
			}
		})
	}
}

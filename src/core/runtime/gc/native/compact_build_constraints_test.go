package gc

import (
	"go/build/constraint"
	"os"
	"strings"
	"testing"
)

// Inspect source selection without compiling every experimental tag combination.
// The heap selector is the authority for the compact mode family.
func TestCompactDiagnosticBuildConstraints(t *testing.T) {
	read := func(name string) constraint.Expr {
		t.Helper()
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		line, _, _ := strings.Cut(string(data), "\n")
		e, err := constraint.Parse(line)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return e
	}
	heap := read("direct_heap_compact.go")
	view := read("compact_view_test.go")
	cohort := read("compact_cohort_diagnostic.go")
	shadow := read("compact_shadow_diagnostic.go")
	var modes []string
	seen := map[string]bool{}
	var walk func(constraint.Expr)
	walk = func(e constraint.Expr) {
		switch e := e.(type) {
		case *constraint.TagExpr:
			if !seen[e.Tag] {
				seen[e.Tag] = true
				modes = append(modes, e.Tag)
			}
		case *constraint.AndExpr:
			walk(e.X)
			walk(e.Y)
		case *constraint.OrExpr:
			walk(e.X)
			walk(e.Y)
		case *constraint.NotExpr:
			walk(e.X)
		}
	}
	walk(heap)
	// No mode, each standalone mode, and each pair guard both omissions and
	// precedence errors that are masked when refstruct accompanies another tag.
	cases := [][]string{nil}
	for i, a := range modes {
		cases = append(cases, []string{a})
		for _, b := range modes[i+1:] {
			cases = append(cases, []string{a, b})
		}
	}
	for _, modes := range cases {
		for bits := 0; bits < 32; bits++ {
			tags := map[string]bool{}
			for _, mode := range modes {
				tags[mode] = true
			}
			for i, tag := range []string{"amd64", "tinygo", "wago_target_tinygo", "wago_gc_compact_cohort", "wago_gc_compact_shadow"} {
				tags[tag] = bits&(1<<i) != 0
			}
			eval := func(tag string) bool { return tags[tag] }
			compact := heap.Eval(eval)
			wantView := compact && tags["amd64"] && !tags["tinygo"] && !tags["wago_target_tinygo"]
			if view.Eval(eval) != wantView || cohort.Eval(eval) != (compact && tags["wago_gc_compact_cohort"]) || shadow.Eval(eval) != (compact && tags["wago_gc_compact_shadow"]) {
				t.Fatalf("inconsistent compact diagnostic selection: %v", tags)
			}
		}
	}
}

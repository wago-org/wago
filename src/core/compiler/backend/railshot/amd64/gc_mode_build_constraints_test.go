package amd64

import (
	"go/build/constraint"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestGCModeBuildConstraintsStayExclusive(t *testing.T) {
	pairs := [][2]string{
		{"gc_helper_only_enabled.go", "gc_helper_only_disabled.go"},
		{"gc_compact_helpers_enabled.go", "gc_compact_helpers_disabled.go"},
		{"gc_compact_reads_enabled.go", "gc_compact_reads_disabled.go"},
		{"gc_compact_alloc_enabled.go", "gc_compact_alloc_disabled.go"},
		{"../../../../runtime/gc/native/compact_view_enabled.go", "../../../../runtime/gc/native/compact_view_disabled.go"},
		{"../../../../runtime/gc/native/direct_heap_compact.go", "../../../../runtime/gc/native/direct_heap_default.go"},
		{"../../../../../wago/gc_compact_runtime_enabled.go", "../../../../../wago/gc_compact_runtime_disabled.go"},
	}
	var exprs [][2]constraint.Expr
	tags := map[string]bool{}
	tagPattern := regexp.MustCompile(`wago_gc_[a-z_]+`)
	for _, pair := range pairs {
		var parsed [2]constraint.Expr
		for i, p := range pair {
			data, err := os.ReadFile(filepath.Clean(p))
			if err != nil {
				t.Fatal(err)
			}
			line := strings.SplitN(string(data), "\n", 2)[0]
			parsed[i], err = constraint.Parse(line)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			for _, tag := range tagPattern.FindAllString(line, -1) {
				tags[tag] = true
			}
		}
		exprs = append(exprs, parsed)
	}
	var modes []string
	for tag := range tags {
		modes = append(modes, tag)
	}
	check := func(enabled map[string]bool) {
		eval := func(tag string) bool { return tag == "amd64" || tag == "linux" || enabled[tag] }
		for i, pair := range exprs {
			if pair[0].Eval(eval) == pair[1].Eval(eval) {
				t.Fatalf("nonexclusive GC mode files %v with tags %v", pairs[i], enabled)
			}
		}
	}
	check(nil)
	for _, a := range modes {
		check(map[string]bool{a: true})
		for _, b := range modes {
			check(map[string]bool{a: true, b: true})
		}
	}
	check(tags)
}

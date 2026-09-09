package wagobench

import (
	"crypto/sha256"
	"testing"

	"github.com/wago-org/wago"
)

func TestAbsoluteCostCodeAudit(t *testing.T) {
	check := func(name string, c *wago.Compiled) {
		h := sha256.New()
		n, err := c.WriteCodeTo(h)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("CODE %s bytes=%d sha256=%x", name, n, h.Sum(nil))
	}
	for _, m := range loadCorpus(t) {
		if m.name() != "coremark" && m.name() != "isa_bulk_mem" {
			continue
		}
		c, err := wago.Compile(nil, m.bytes)
		if err != nil {
			t.Fatal(err)
		}
		check(m.name(), c)
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range pluginCorpus(t) {
		if m.name() != "ruby" && m.name() != "wasm3" {
			continue
		}
		rt, mod, cleanup := compilePluginCorpus(t, m, nil)
		check(m.name(), mod.Compiled())
		if err := mod.Close(); err != nil {
			t.Fatal(err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
		cleanup()
	}
}

func TestAllExecutableCodeAudit(t *testing.T) {
	for _, m := range loadCorpus(t) {
		if !m.supports("Exec") || (len(m.Exec) == 0 && len(m.SemanticExec) == 0) {
			continue
		}
		c, err := wago.Compile(nil, m.bytes)
		if err != nil {
			t.Fatalf("%s: %v", m.name(), err)
		}
		h := sha256.New()
		n, err := c.WriteCodeTo(h)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("EXEC_CODE %s bytes=%d sha256=%x", m.name(), n, h.Sum(nil))
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

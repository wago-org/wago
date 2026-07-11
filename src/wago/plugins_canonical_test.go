package wago

import "testing"

func TestCanonicalPluginID(t *testing.T) {
	cases := map[string]string{
		"github.com/wago-org/wasi": "wago-org/wasi",
		"wago-org/wasi":            "wago-org/wasi",
		"github.com/a/b/c":         "a/b/c",
		"timer":                    "timer",
		"gitlab.com/x/y":           "gitlab.com/x/y", // only github.com is stripped
	}
	for in, want := range cases {
		if got := canonicalPluginID(in); got != want {
			t.Errorf("canonicalPluginID(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRegisterLookupInterchangeable reproduces the reported bug: a plugin that
// registers under its full module path must be found by the short GitHub-relative
// id that wago.json and `--plugin` use (and vice-versa), and it must list under
// the canonical id.
func TestRegisterLookupInterchangeable(t *testing.T) {
	const full = "github.com/wago-org/canon-test"
	const short = "wago-org/canon-test"
	RegisterExtension(full, func() Extension { return tripleExt{} })
	t.Cleanup(func() {
		pluginMu.Lock()
		delete(pluginReg, short)
		pluginMu.Unlock()
	})

	for _, q := range []string{full, short} {
		if _, ok := NewExtension(q); !ok {
			t.Errorf("NewExtension(%q) not found; registration/lookup ids disagree", q)
		}
	}

	found := false
	for _, n := range RegisteredPluginNames() {
		if n == short {
			found = true
		}
		if n == full {
			t.Errorf("RegisteredPluginNames returned the full path %q; want canonical %q", full, short)
		}
	}
	if !found {
		t.Errorf("RegisteredPluginNames missing canonical id %q", short)
	}
}

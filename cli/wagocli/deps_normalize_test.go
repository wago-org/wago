package wagocli

import "testing"

func TestNormalizeModuleRef(t *testing.T) {
	cases := map[string]string{
		"wago-org/wasi":                  "github.com/wago-org/wasi",
		"github.com/wago-org/wasi":       "github.com/wago-org/wasi",
		"wago-org/wasi@1.2.3":            "github.com/wago-org/wasi@1.2.3",
		"github.com/wago-org/wasi@1.2.3": "github.com/wago-org/wasi@1.2.3",
		"gitlab.com/foo/bar":             "gitlab.com/foo/bar", // host already present
		"wasi":                           "wasi",               // bare short: no slash, untouched
		"":                               "",
		"  wago-org/wasi  ":              "github.com/wago-org/wasi", // trimmed
	}
	for in, want := range cases {
		if got := normalizeModuleRef(in); got != want {
			t.Errorf("normalizeModuleRef(%q) = %q, want %q", in, got, want)
		}
	}
}

package plugin

import (
	"strings"
	"testing"
)

func TestArgumentParsing(t *testing.T) {
	name, version := SplitVersion("example/pkg@v1.2.3")
	if name != "example/pkg" || version != "v1.2.3" {
		t.Fatalf("SplitVersion = %q %q", name, version)
	}
	name, version = SplitVersion("a@b@v1")
	if name != "a@b" || version != "v1" {
		t.Fatalf("SplitVersion scoped = %q %q", name, version)
	}
	if got := strings.Join(SplitCommaList(" a, ,b ,, c "), ","); got != "a,b,c" {
		t.Fatalf("SplitCommaList = %q", got)
	}
}

func TestNormalizeModuleRef(t *testing.T) {
	cases := map[string]string{
		"wago-org/wasi":                  "github.com/wago-org/wasi",
		"github.com/wago-org/wasi":       "github.com/wago-org/wasi",
		"wago-org/wasi@1.2.3":            "github.com/wago-org/wasi@1.2.3",
		"github.com/wago-org/wasi@1.2.3": "github.com/wago-org/wasi@1.2.3",
		"gitlab.com/foo/bar":             "gitlab.com/foo/bar",
		"wasi":                           "wasi",
		"":                               "",
		"  wago-org/wasi  ":              "github.com/wago-org/wasi",
	}
	for input, want := range cases {
		if got := NormalizeModuleRef(input); got != want {
			t.Errorf("NormalizeModuleRef(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestModuleVersionAndPluralHelpers(t *testing.T) {
	for _, test := range []struct{ spec, module, version string }{
		{"example.test/plugin@v1.2.3", "example.test/plugin", "v1.2.3"},
		{"example.test/plugin", "example.test/plugin", ""},
		{"@scope/plugin", "@scope/plugin", ""},
	} {
		module, version := SplitModuleVersion(test.spec)
		if module != test.module || version != test.version {
			t.Fatalf("SplitModuleVersion(%q) = %q, %q", test.spec, module, version)
		}
	}
	if Plural(1) != "" || Plural(0) != "s" || Plural(2) != "s" {
		t.Fatal("Plural helper changed")
	}
}

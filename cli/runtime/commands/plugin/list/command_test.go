package list

import (
	"strings"
	"testing"
)

func TestPresentationHelpers(t *testing.T) {
	if got := entry("wago-org/wasi", "1.0.0"); got != "wago-org/wasi@1.0.0" {
		t.Fatalf("entry = %q", got)
	}
	if got := packageRoot("wago-org/wasi/unstable"); got != "wago-org/wasi" {
		t.Fatalf("package root = %q", got)
	}
	if got := childName("wago-org/wasi", "wago-org/wasi/unstable"); got != "wasi/unstable" {
		t.Fatalf("child name = %q", got)
	}
	items := []item{
		{name: "wago-org/wasi", version: "1.0.0"},
		{name: "wago-org/wasi/p1", version: "1.0.0"},
		{name: "wago-org/wasi/unstable", version: "1.0.0"},
	}
	for _, scope := range []string{"local", "global"} {
		got := strings.Join(lines(scope, items), "\n")
		want := "Installed plugins (" + scope + ")\n\nwago-org/wasi@1.0.0\n - wasi/p1@1.0.0\n - wasi/unstable@1.0.0"
		if got != want {
			t.Fatalf("%s plugin list = %q, want %q", scope, got, want)
		}
	}
}

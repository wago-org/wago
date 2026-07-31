package plugin

import "testing"

func TestResolvePackagesSupportsMultiplePackages(t *testing.T) {
	resolver := func(name string) (string, error) {
		return "github.com/registry/" + name, nil
	}
	packages, err := ResolvePackages([]string{
		"wago-org/wasi@v1.2.3",
		"github.com/acme/log",
		"wago-org/wasi@v1.2.3",
	}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 ||
		packages[0].Module != "github.com/wago-org/wasi" ||
		packages[0].Requested != "v1.2.3" ||
		packages[1].Module != "github.com/acme/log" {
		t.Fatalf("resolved packages = %#v", packages)
	}
	if _, err := ResolvePackages([]string{"wago-org/wasi@v1", "wago-org/wasi@v2"}, resolver); err == nil {
		t.Fatal("conflicting package versions were accepted")
	}
}

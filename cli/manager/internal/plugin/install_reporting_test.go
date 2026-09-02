package plugin

import (
	"context"
	"reflect"
	"testing"

	"github.com/wago-org/wago/cli/internal/project"
)

func TestReportCompletedPluginInstallsCountsRequestedPackagesOnce(t *testing.T) {
	lock := project.NewLockDocument()
	lock.Plugins["github.com/wago-org/wasi/p1"] = project.LockEntry{
		Direct: true,
		Source: project.PluginSource{Module: "github.com/wago-org/wasi", Version: "v0.2.1"},
	}
	lock.Plugins["github.com/wago-org/wasi/p2"] = project.LockEntry{
		Direct: true,
		Source: project.PluginSource{Module: "github.com/wago-org/wasi", Version: "v0.2.1"},
	}
	lock.Plugins["github.com/wago-org/component-model"] = project.LockEntry{
		Source: project.PluginSource{Module: "github.com/wago-org/component-model", Version: "v0.1.4"},
	}
	lock.Plugins["github.com/acme/existing"] = project.LockEntry{
		Direct: true,
		Source: project.PluginSource{Module: "github.com/acme/existing", Version: "v1.0.0"},
	}

	var got []project.PluginSource
	record := func(_ context.Context, module, version string) {
		got = append(got, project.PluginSource{Module: module, Version: version})
	}

	reportCompletedPluginInstalls(context.Background(), []string{
		"wago-org/wasi/p1@^0.2.0",
		"github.com/wago-org/wasi/p2@^0.2.0",
	}, lock, record)

	want := []project.PluginSource{{Module: "github.com/wago-org/wasi", Version: "v0.2.1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reported installs = %#v, want %#v", got, want)
	}
}

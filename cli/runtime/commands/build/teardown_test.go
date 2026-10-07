package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

type teardownEnvironment struct{}

func (teardownEnvironment) ProfileFlags() []command.Flag { return nil }

func (teardownEnvironment) LoadRuntime(config *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	runtime := wago.NewRuntime(wago.WithRuntimeConfig(config), wago.WithGuestArguments(guestArgs))
	definition := wago.PluginDefinition{
		ID: "example.com/cli/build-teardown", Version: "1.0.0",
		Provenance: wago.PluginProvenance{Repository: "https://example.com/cli/build-teardown", License: "MIT"},
		Authorities: []wago.AuthorityRequest{{
			Name: wago.AuthorityModuleCloseObserve, Mode: wago.AuthorityRequired, Reason: "verify build teardown",
		}},
	}
	digest, err := wago.DefinitionDigest(definition)
	if err != nil {
		panic(err)
	}
	plugins := wago.PluginSet{
		Providers: []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return teardownPlugin{} }}},
		Selections: []wago.PluginSelection{{
			ID: definition.ID, DefinitionDigest: digest, Direct: true,
			Dependencies: map[string]string{},
			Grants:       []wago.AuthorityGrant{{Name: wago.AuthorityModuleCloseObserve}},
		}},
	}
	if err := runtime.LoadPlugins(context.Background(), plugins); err != nil {
		panic(err)
	}
	return runtime
}

type teardownPlugin struct{}

func (teardownPlugin) Register(registrar *wago.Registrar) error {
	observer, err := registrar.ModuleCloseObserver()
	if err != nil {
		return err
	}
	if err := observer.Observe(func(wago.ModuleCloseEvent) {
		_ = os.WriteFile(os.Getenv("WAGO_BUILD_MODULE_CLOSED"), []byte("closed"), 0o600)
	}); err != nil {
		return err
	}
	return registrar.Lifecycle(wago.PluginLifecycle{Stop: func(context.Context) error {
		return errors.Join(
			os.WriteFile(os.Getenv("WAGO_BUILD_PLUGIN_STOPPED"), []byte("stopped"), 0o600),
			errors.New("forced teardown failure"),
		)
	}})
}

func TestBuildTeardownFailurePreservesExistingArtifact(t *testing.T) {
	if os.Getenv("WAGO_BUILD_TEARDOWN_CHILD") == "1" {
		Command(teardownEnvironment{}).Run(command.NewContext(
			[]string{os.Getenv("WAGO_BUILD_INPUT")},
			map[string]string{"output": os.Getenv("WAGO_BUILD_OUTPUT")}, nil,
		))
		return
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wasm")
	if err := os.WriteFile(input, []byte{0, 'a', 's', 'm', 1, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output.wago")
	previous := []byte("prior artifact")
	if err := os.WriteFile(output, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	moduleMarker := filepath.Join(dir, "module-closed")
	stoppedMarker := filepath.Join(dir, "plugin-stopped")
	child := exec.Command(os.Args[0], "-test.run=^TestBuildTeardownFailurePreservesExistingArtifact$", "-test.count=1")
	child.Env = append(os.Environ(),
		"WAGO_BUILD_TEARDOWN_CHILD=1", "WAGO_BUILD_INPUT="+input, "WAGO_BUILD_OUTPUT="+output,
		"WAGO_BUILD_MODULE_CLOSED="+moduleMarker, "WAGO_BUILD_PLUGIN_STOPPED="+stoppedMarker,
	)
	combined, err := child.CombinedOutput()
	if err == nil || !bytes.Contains(combined, []byte("forced teardown failure")) {
		t.Fatalf("build error = %v: %s; want teardown failure", err, combined)
	}
	if got, err := os.ReadFile(output); err != nil || !bytes.Equal(got, previous) {
		t.Fatalf("teardown failure changed output to %q, %v; want %q", got, err, previous)
	}
	if _, err := os.Stat(moduleMarker); err != nil {
		t.Fatalf("build did not close its module before exit: %v\n%s", err, combined)
	}
	if _, err := os.Stat(stoppedMarker); err != nil {
		t.Fatalf("build exited without completing plugin teardown: %v\n%s", err, combined)
	}
}

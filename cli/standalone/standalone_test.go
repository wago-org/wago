package standalone

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago"
)

type teardownTestPlugin struct {
	moduleClosed chan struct{}
	stopStarted  chan struct{}
	stopRelease  chan struct{}
	stopped      chan struct{}
}

func (plugin *teardownTestPlugin) Register(registrar *wago.Registrar) error {
	observer, err := registrar.ModuleCloseObserver()
	if err != nil {
		return err
	}
	if err := observer.Observe(func(wago.ModuleCloseEvent) { close(plugin.moduleClosed) }); err != nil {
		return err
	}
	return registrar.Lifecycle(wago.PluginLifecycle{Stop: func(context.Context) error {
		close(plugin.stopStarted)
		<-plugin.stopRelease
		close(plugin.stopped)
		return nil
	}})
}

func TestRunEmptyStartModule(t *testing.T) {
	if code := Run(emptyStartModule(), wago.PluginSet{}, Options{DeferBoundsChecks: true}, []string{"hello"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestReportErrorPreservesExitCodesWithoutHidingTeardownFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"zero exit", &wago.ExitError{Code: 0}, 0},
		{"nonzero exit", &wago.ExitError{Code: 7}, 7},
		{"zero exit with teardown failure", errors.Join(&wago.ExitError{Code: 0}, errors.New("forced teardown failure")), 1},
		{"nonzero exit with teardown failure", errors.Join(&wago.ExitError{Code: 7}, errors.New("forced teardown failure")), 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportError(tc.err, []string{"program"}); got != tc.want {
				t.Fatalf("reportError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

type exitTeardownPlugin struct{ code int32 }

func (plugin exitTeardownPlugin) Register(registrar *wago.Registrar) error {
	imports, err := registrar.HostImports()
	if err != nil {
		return err
	}
	imports.HostFunc("env", "exit", func() { panic(wago.HostExit{Code: plugin.code}) })
	return registrar.Lifecycle(wago.PluginLifecycle{Stop: func(context.Context) error {
		return errors.New("forced teardown failure")
	}})
}

func TestRunAndRunArtifactReportZeroExitTeardownFailure(t *testing.T) {
	// (module (import "env" "exit" (func))
	//   (func (export "_start") (call 0)))
	source := []byte{0, 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		2, 12, 1, 3, 'e', 'n', 'v', 4, 'e', 'x', 'i', 't', 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1,
		10, 6, 1, 4, 0, 0x10, 0, 0x0b}
	artifact, err := CompileArtifact(source, wago.PluginSet{}, Options{DeferBoundsChecks: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []int32{0, 7} {
		definition := wago.PluginDefinition{
			ID: "example.com/cli/standalone-exit-teardown", Version: "1.0.0",
			Provenance: wago.PluginProvenance{Repository: "https://example.com/cli/standalone-exit-teardown", License: "MIT"},
			Authorities: []wago.AuthorityRequest{{
				Name: wago.AuthorityHostImportDefine, Mode: wago.AuthorityRequired,
				Reason: "request a guest exit", Scope: wago.AuthorityScope{Modules: []string{"env"}},
			}},
		}
		digest, err := wago.DefinitionDigest(definition)
		if err != nil {
			t.Fatal(err)
		}
		set := wago.PluginSet{
			Providers: []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return exitTeardownPlugin{code: code} }}},
			Selections: []wago.PluginSelection{{
				ID: definition.ID, DefinitionDigest: digest, Direct: true,
				Dependencies: map[string]string{},
				Grants: []wago.AuthorityGrant{{
					Name: wago.AuthorityHostImportDefine, Scope: wago.AuthorityScope{Modules: []string{"env"}},
				}},
			}},
		}
		want := int(code)
		if code == 0 {
			want = 1
		}
		for _, mode := range []struct {
			name string
			run  func() int
		}{
			{"source", func() int { return Run(source, set, Options{DeferBoundsChecks: true}, nil) }},
			{"artifact", func() int { return RunArtifact(artifact, set, Options{DeferBoundsChecks: true}, nil) }},
		} {
			t.Run(mode.name+"/"+strconv.Itoa(int(code)), func(t *testing.T) {
				if got := mode.run(); got != want {
					t.Fatalf("exit code = %d, want %d", got, want)
				}
			})
		}
	}
}

func TestExecuteClosesModuleAndWaitsForRuntimeTeardown(t *testing.T) {
	plugin := &teardownTestPlugin{
		moduleClosed: make(chan struct{}), stopStarted: make(chan struct{}),
		stopRelease: make(chan struct{}), stopped: make(chan struct{}),
	}
	definition := wago.PluginDefinition{
		ID: "example.com/cli/teardown", Version: "1.0.0",
		Provenance: wago.PluginProvenance{Repository: "https://example.com/cli/teardown", License: "MIT"},
		Authorities: []wago.AuthorityRequest{{
			Name: wago.AuthorityModuleCloseObserve, Mode: wago.AuthorityRequired, Reason: "verify CLI module teardown",
		}},
	}
	digest, err := wago.DefinitionDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	set := wago.PluginSet{
		Providers: []wago.PluginProvider{{Definition: definition, New: func() wago.Plugin { return plugin }}},
		Selections: []wago.PluginSelection{{
			ID: definition.ID, DefinitionDigest: digest, Direct: true,
			Dependencies: map[string]string{},
			Grants:       []wago.AuthorityGrant{{Name: wago.AuthorityModuleCloseObserve}},
		}},
	}
	done := make(chan error, 1)
	go func() {
		done <- execute(emptyStartModule(), set, Options{DeferBoundsChecks: true}, []string{"hello"})
	}()

	select {
	case <-plugin.stopStarted:
	case <-time.After(5 * time.Second):
		close(plugin.stopRelease)
		t.Fatal("execute did not start runtime teardown")
	}
	select {
	case <-plugin.moduleClosed:
	default:
		close(plugin.stopRelease)
		t.Fatal("execute began runtime teardown before closing the module")
	}
	select {
	case err := <-done:
		close(plugin.stopRelease)
		t.Fatalf("execute returned before plugin teardown completed: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(plugin.stopRelease)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("execute did not return after teardown was released")
	}
	select {
	case <-plugin.stopped:
	default:
		t.Fatal("execute returned without completing plugin teardown")
	}
}

func TestCompileAndRunArtifact(t *testing.T) {
	options := Options{DeferBoundsChecks: true}
	artifact, err := CompileArtifact(emptyStartModule(), wago.PluginSet{}, options)
	if err != nil {
		t.Fatal(err)
	}
	if !wago.IsCompiled(artifact) {
		t.Fatal("CompileArtifact did not return a .wago artifact")
	}
	if code := RunArtifact(artifact, wago.PluginSet{}, options, []string{"hello"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestExecuteRejectsModuleWithoutFunctionExports(t *testing.T) {
	empty := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	if err := execute(empty, wago.PluginSet{}, Options{DeferBoundsChecks: true}, nil); err == nil || err.Error() != "module exports no functions" {
		t.Fatalf("execute error = %v", err)
	}
}

func TestExecuteInvokesExportWithTypedArgs(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = original })

	if err := execute(addModule(), wago.PluginSet{}, Options{Invoke: "add", DeferBoundsChecks: true}, []string{"add", "20", "22"}); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output := make([]byte, 64)
	n, err := read.Read(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(output[:n]); got != "42\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestExecuteSelectsSoleExportWithTypedArgs(t *testing.T) {
	output := captureStdout(t, func() {
		if err := execute(addModule(), wago.PluginSet{}, Options{DeferBoundsChecks: true}, []string{"add", "20", "22"}); err != nil {
			t.Fatal(err)
		}
	})
	if output != "42\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestExecuteSelectsMainBeforeOtherExports(t *testing.T) {
	output := captureStdout(t, func() {
		if err := execute(mainAndOtherModule(), wago.PluginSet{}, Options{DeferBoundsChecks: true}, []string{"program"}); err != nil {
			t.Fatal(err)
		}
	})
	if output != "7\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestExecuteRejectsWrongInvokeArguments(t *testing.T) {
	err := execute(addModule(), wago.PluginSet{}, Options{Invoke: "add", DeferBoundsChecks: true}, []string{"add", "20"})
	if err == nil || err.Error() != "expected 2 arg(s), got 1" {
		t.Fatalf("execute error = %v", err)
	}
}

func TestExecuteRejectsV128ResultWithoutPanicking(t *testing.T) {
	err := execute(v128ResultModule(), wago.PluginSet{}, Options{DeferBoundsChecks: true}, []string{"program"})
	if err == nil || !strings.Contains(err.Error(), "result 0 is v128") {
		t.Fatalf("execute error = %v", err)
	}
}

func TestExecuteAppliesOptimizationKnobs(t *testing.T) {
	knob := wago.NewRuntimeConfig().OptimizationInfos()[0]
	config, err := runtimeConfig(Options{DeferBoundsChecks: true, OptimizationKnobs: map[string]bool{knob.Name: !knob.On}})
	if err != nil {
		t.Fatal(err)
	}
	if got := config.OptimizationInfos()[0].On; got == knob.On {
		t.Fatalf("optimization %s remained %v", knob.Name, got)
	}
}

func TestExecuteRejectsUnknownOptimization(t *testing.T) {
	err := execute(emptyStartModule(), wago.PluginSet{}, Options{OptimizationKnobs: map[string]bool{"not-a-knob": true}}, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown`) || !strings.Contains(err.Error(), `not-a-knob`) {
		t.Fatalf("execute error = %v", err)
	}
}

func TestExecuteSupportsCore3(t *testing.T) {
	err := execute(tailCallStartModule(), wago.PluginSet{}, Options{Core: 3, DeferBoundsChecks: true}, []string{"hello"})
	if wago.CoreFeaturesV3&^wago.SupportedFeatures() != 0 {
		if err == nil {
			t.Fatal("execute Core 3 module succeeded on an incomplete Core 3 backend")
		}
		return
	}
	if err != nil {
		t.Fatalf("execute Core 3 module: %v", err)
	}
}

func TestRuntimeConfigUsesBakedFunctionWorkers(t *testing.T) {
	config, err := runtimeConfig(Options{Core: 2, DeferBoundsChecks: true, FunctionWorkers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got := config.FunctionWorkers(); got != 4 {
		t.Fatalf("function workers = %d, want 4", got)
	}
	if got := config.CoreFeatures(); got != wago.CoreFeaturesV2 {
		t.Fatalf("baked Core 2 features = %s, want %s", got, wago.CoreFeaturesV2)
	}
}

func emptyStartModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 2, 1, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0,
		10, 4, 1, 2, 0, 0x0b,
	}
}

func addModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 7, 1, 0x60, 2, 0x7f, 0x7f, 1, 0x7f,
		3, 2, 1, 0,
		7, 7, 1, 3, 'a', 'd', 'd', 0, 0,
		10, 9, 1, 7, 0, 0x20, 0, 0x20, 1, 0x6a, 0x0b,
	}
}

func mainAndOtherModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7f,
		3, 2, 1, 0,
		7, 16, 2, 4, 'm', 'a', 'i', 'n', 0, 0, 5, 'o', 't', 'h', 'e', 'r', 0, 0,
		10, 6, 1, 4, 0, 0x41, 7, 0x0b,
	}
}

func v128ResultModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 5, 1, 0x60, 0, 1, 0x7b,
		3, 2, 1, 0,
		7, 5, 1, 1, 'v', 0, 0,
		10, 22, 1, 20, 0, 0xfd, 0x0c,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0x0b,
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = original })
	run()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output := make([]byte, 256)
	n, err := read.Read(output)
	if err != nil {
		t.Fatal(err)
	}
	return string(output[:n])
}

func tailCallStartModule() []byte {
	return []byte{
		'\x00', 'a', 's', 'm', 1, 0, 0, 0,
		1, 4, 1, 0x60, 0, 0,
		3, 3, 2, 0, 0,
		7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 1,
		10, 9, 2, 2, 0, 0x0b, 4, 0, 0x12, 0, 0x0b,
	}
}

func BenchmarkRuntimeConfig(b *testing.B) {
	for _, core := range []int{0, 2} {
		name := "Default"
		if core == 2 {
			name = "Core2"
		}
		b.Run(name, func(b *testing.B) {
			options := Options{Core: core, DeferBoundsChecks: true, Features: wago.NewRuntimeConfig().CoreFeatures(), FeaturesSet: true}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := runtimeConfig(options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCompileArtifact(b *testing.B) {
	source := addModule()
	options := Options{Features: wago.CoreFeaturesV2, FeaturesSet: true, DeferBoundsChecks: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := CompileArtifact(source, wago.PluginSet{}, options); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRuntimeConfigUsesBakedFeatures(t *testing.T) {
	for _, test := range []struct {
		name    string
		options Options
		want    wago.CoreFeatures
	}{
		{"default", Options{}, wago.NewRuntimeConfig().CoreFeatures()},
		{"unset mask", Options{Features: wago.CoreFeaturesV1}, wago.NewRuntimeConfig().CoreFeatures()},
		{"empty mask", Options{FeaturesSet: true}, 0},
		{"SIMD disabled", Options{FeaturesSet: true, Features: wago.CoreFeaturesV2 &^ wago.CoreFeatureSIMD}, wago.CoreFeaturesV2 &^ wago.CoreFeatureSIMD},
		{"explicit core overrides mask", Options{Core: 2, FeaturesSet: true}, wago.CoreFeaturesV2},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := runtimeConfig(test.options)
			if err != nil {
				t.Fatal(err)
			}
			if got := config.CoreFeatures(); got != test.want {
				t.Fatalf("features = %s, want %s", got, test.want)
			}
		})
	}
	if _, err := runtimeConfig(Options{FeaturesSet: true, Features: wago.CoreFeatures(1 << 63)}); err == nil {
		t.Fatal("unknown feature mask was accepted")
	}
	if _, err := runtimeConfig(Options{Core: 99, FeaturesSet: true}); err == nil {
		t.Fatal("unknown Core selection was accepted")
	}
}

func TestRuntimeConfigBakedDefaultsDoNotAllocateMore(t *testing.T) {
	options := Options{DeferBoundsChecks: true}
	measure := func() float64 {
		return testing.AllocsPerRun(10, func() {
			if _, err := runtimeConfig(options); err != nil {
				panic(err)
			}
		})
	}
	before := measure()
	options.Features, options.FeaturesSet = wago.NewRuntimeConfig().CoreFeatures(), true
	if after := measure(); after > before {
		t.Fatalf("baked default features allocate %g times, want at most %g", after, before)
	}
}

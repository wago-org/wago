package standalone

import (
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestRunEmptyStartModule(t *testing.T) {
	if code := Run(emptyStartModule(), wago.PluginSet{}, Options{DeferBoundsChecks: true}, []string{"hello"}); code != 0 {
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

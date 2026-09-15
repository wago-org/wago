package wagobench

// Executing JIT code in a child process gives every case fresh compiler state
// and lets the parent kill a native loop which cannot yield to Go's timeout.

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const corpusChildMarker = "WAGO_CORPUS_OK"

func TestCorpus(t *testing.T) {
	if os.Getenv("WAGO_CORPUS_CHILD") == "1" {
		t.Skip("corpus child is selected directly")
	}
	for _, m := range loadCorpus(t) {
		for _, stage := range corpusStages(m) {
			if stage != "Exec" {
				t.Run(m.ID+"/"+stage, func(t *testing.T) {
					runCorpusChild(t, m.ID, stage, "", "explicit")
				})
				continue
			}
			for _, invocation := range m.Exec {
				if invocation.Want == nil {
					t.Fatalf("%s.%s has no exact result oracle", m.ID, invocation.Export)
				}
				t.Run(m.ID+"/Exec/"+invocation.Export, func(t *testing.T) {
					runCorpusChild(t, m.ID, stage, invocation.Export, "explicit")
					if corpusGuardEnabled() {
						runCorpusChild(t, m.ID, stage, invocation.Export, "guard")
					}
				})
			}
		}
	}
}

func corpusStages(m corpusModule) []string {
	if len(m.Stages) != 0 {
		stages := make([]string, 0, len(m.Stages))
		for _, stage := range m.Stages {
			if stage != "CommandExec" {
				stages = append(stages, stage)
			}
		}
		return stages
	}
	stages := []string{"Decode", "Validate", "Compile", "CompileFull", "Instantiate"}
	if len(m.Exec) != 0 {
		stages = append(stages, "Exec")
	}
	return stages
}

func runCorpusChild(t *testing.T, id, stage, export, bounds string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCorpusChild$", "-test.v", "-wago.corpus="+*corpusSelector)
	cmd.Env = append(os.Environ(),
		"WAGO_CORPUS_CHILD=1",
		"WAGO_CORPUS_ID="+id,
		"WAGO_CORPUS_STAGE="+stage,
		"WAGO_CORPUS_EXPORT="+export,
		"WAGO_CORPUS_BOUNDS="+bounds,
	)
	var output strings.Builder
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start corpus child: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	const timeout = 15 * time.Second
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("corpus child failed: %v\n%s", err, output.String())
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("corpus child timed out after %s: %s/%s/%s", timeout, id, stage, export)
	}
	if !strings.Contains(output.String(), corpusChildMarker) {
		t.Fatalf("corpus child did not report completion:\n%s", output.String())
	}
}

func TestCorpusChild(t *testing.T) {
	if os.Getenv("WAGO_CORPUS_CHILD") != "1" {
		t.Skip("parent-only helper")
	}
	id := os.Getenv("WAGO_CORPUS_ID")
	var selected *corpusModule
	for _, m := range loadCorpus(t) {
		if m.ID == id {
			m := m
			selected = &m
			break
		}
	}
	if selected == nil {
		t.Fatalf("unknown selected corpus benchmark %q", id)
	}
	runCorpusStage(t, *selected, os.Getenv("WAGO_CORPUS_STAGE"), os.Getenv("WAGO_CORPUS_EXPORT"), os.Getenv("WAGO_CORPUS_BOUNDS"))
	fmt.Println(corpusChildMarker)
}

func runCorpusStage(t *testing.T, m corpusModule, stage, export, bounds string) {
	t.Helper()
	decoded, err := wasm.DecodeModule(m.bytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stage == "Decode" {
		return
	}
	if err := wasm.ValidateModule(decoded); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if stage == "Validate" {
		return
	}
	if stage == "Compile" {
		if _, err := benchCompileModule(decoded); err != nil {
			t.Fatalf("compile: %v", err)
		}
		return
	}
	cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
	if bounds == "guard" {
		cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
	}
	compiled, err := wago.Compile(cfg, m.bytes)
	if err != nil {
		t.Fatalf("compile full: %v", err)
	}
	if stage == "CompileFull" {
		return
	}
	instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: hostStubs(compiled)})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer instance.Close()
	if stage == "Instantiate" {
		return
	}
	if stage != "Exec" {
		t.Fatalf("unknown corpus stage %q", stage)
	}
	if m.Init != "" {
		if _, err := instance.Invoke(m.Init); err != nil {
			t.Fatalf("initialize %s: %v", m.Init, err)
		}
	}
	for _, invocation := range m.Exec {
		if invocation.Export != export {
			continue
		}
		args := make([]uint64, len(invocation.Args))
		for i, arg := range invocation.Args {
			args[i] = wago.I32(arg)
		}
		got, err := instance.Invoke(export, args...)
		if err != nil {
			t.Fatalf("invoke %s: %v", export, err)
		}
		if !slices.Equal(got, invocation.Want) {
			t.Fatalf("invoke %s results = %v, want %v", export, got, invocation.Want)
		}
		return
	}
	t.Fatalf("export %q is not declared by %s", export, m.ID)
}

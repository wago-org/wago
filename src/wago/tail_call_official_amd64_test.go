//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const stagedDirectTailDeltaPath = "tests/spec-v3-staged-return-call.json"

type stagedTailFileDelta struct {
	Schema        int              `json:"schema"`
	SuiteRevision string           `json:"suite_revision"`
	File          string           `json:"file"`
	Counts        stagedSpecCounts `json:"counts"`
}

func compileStagedTail(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TailCalls = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedOfficialReturnCall(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialCoreJSON(t, "", "return_call", &script)
	var counts stagedSpecCounts
	var current *Instance
	var live []*Instance
	var compiled []*Compiled
	defer func() {
		for i := len(live) - 1; i >= 0; i-- {
			_ = live[i].Close()
		}
		for i := len(compiled) - 1; i >= 0; i-- {
			_ = compiled[i].Close()
		}
	}()
	noop := HostFunc(func(HostModule, []uint64, []uint64) {})
	standard := Imports{
		"spectest.print": noop, "spectest.print_i32": noop, "spectest.print_i64": noop,
		"spectest.print_f32": noop, "spectest.print_f64": noop,
		"spectest.print_i32_f32": noop, "spectest.print_f64_f64": noop,
	}

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("return_call.wast:%d read module: %v", cmd.Line, err)
				current = nil
				continue
			}
			if counts.Commands == 1 {
				if public, err := Compile(nil, data); err == nil {
					_ = public.Close()
					counts.Failures++
					t.Errorf("return_call.wast:%d public compile unexpectedly admitted tail calls", cmd.Line)
				}
			}
			c, err := compileStagedTail(data)
			if err != nil {
				counts.UnexpectedCompileRejects++
				counts.Failures++
				t.Errorf("return_call.wast:%d compile: %v", cmd.Line, err)
				current = nil
				continue
			}
			in, err := instantiateCore(c, InstantiateOptions{Imports: standard})
			if err != nil {
				_ = c.Close()
				counts.UnexpectedLinkRejects++
				counts.Failures++
				t.Errorf("return_call.wast:%d instantiate: %v", cmd.Line, err)
				current = nil
				continue
			}
			compiled = append(compiled, c)
			live = append(live, in)
			current = in
			counts.ModulesPassed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("return_call.wast:%d read invalid module: %v", cmd.Line, err)
				continue
			}
			c, err := compileStagedTail(data)
			if err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("return_call.wast:%d invalid module compiled: %s", cmd.Line, cmd.Text)
				continue
			}
			counts.ExpectedInvalid++
		case "assert_return":
			if current == nil || cmd.Action.Type != "invoke" {
				counts.Failures++
				t.Errorf("return_call.wast:%d action has no live module", cmd.Line)
				continue
			}
			args := make([]uint64, len(cmd.Action.Args))
			valid := true
			for i, arg := range cmd.Action.Args {
				args[i], valid = stagedSpecScalar(arg)
				if !valid {
					break
				}
			}
			if !valid {
				counts.Failures++
				t.Errorf("return_call.wast:%d unsupported argument", cmd.Line)
				continue
			}
			got, callErr := current.Invoke(cmd.Action.Field, args...)
			if callErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("return_call.wast:%d result = %v, err=%v, want %v", cmd.Line, got, callErr, cmd.Expected)
				continue
			}
			matched := true
			for i := range got {
				if !stagedSpecMatch(got[i], cmd.Expected[i]) {
					matched = false
					break
				}
			}
			if !matched {
				counts.Failures++
				t.Errorf("return_call.wast:%d result = %v, want %v", cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("return_call.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 || counts.Failures != 0 {
		t.Fatalf("staged return_call has hidden gaps: %+v", counts)
	}
	delta := stagedTailFileDelta{Schema: 1, SuiteRevision: stagedRelease3Revision, File: "return_call", Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedDirectTailDeltaPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("WAGO_UPDATE_STAGED_SPEC") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("staged return_call delta changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact command accounting\n%s", got)
	}
	t.Logf("staged return_call: commands=%d modules=%d assertions=%d expected-invalid=%d",
		counts.Commands, counts.ModulesPassed, counts.AssertionsPassed, counts.ExpectedInvalid)
}

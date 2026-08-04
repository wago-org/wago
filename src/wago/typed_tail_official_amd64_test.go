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

const stagedTypedTailDeltaPath = "tests/spec-v3-staged-return-call-ref.json"

type stagedTypedTailDelta struct {
	Schema        int              `json:"schema"`
	SuiteRevision string           `json:"suite_revision"`
	File          string           `json:"file"`
	Counts        stagedSpecCounts `json:"counts"`
}

func TestStagedOfficialReturnCallRef(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialCoreJSON(t, "", "return_call_ref", &script)
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

	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("return_call_ref.wast:%d read module: %v", cmd.Line, err)
				current = nil
				continue
			}
			c, err := compileStagedTypedTail(data)
			if err != nil {
				counts.UnexpectedCompileRejects++
				counts.Failures++
				t.Errorf("return_call_ref.wast:%d compile: %v", cmd.Line, err)
				current = nil
				continue
			}
			in, err := instantiateCore(c, InstantiateOptions{})
			if err != nil {
				_ = c.Close()
				counts.UnexpectedLinkRejects++
				counts.Failures++
				t.Errorf("return_call_ref.wast:%d instantiate: %v", cmd.Line, err)
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
				t.Errorf("return_call_ref.wast:%d read invalid module: %v", cmd.Line, err)
				continue
			}
			c, err := compileStagedTypedTail(data)
			if err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("return_call_ref.wast:%d invalid module compiled: %s", cmd.Line, cmd.Text)
				continue
			}
			counts.ExpectedInvalid++
		case "assert_return", "assert_trap":
			if current == nil || cmd.Action.Type != "invoke" {
				counts.Failures++
				t.Errorf("return_call_ref.wast:%d action has no live module", cmd.Line)
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
				t.Errorf("return_call_ref.wast:%d unsupported argument", cmd.Line)
				continue
			}
			got, callErr := current.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_trap" {
				if callErr == nil {
					counts.Failures++
					t.Errorf("return_call_ref.wast:%d expected trap: %s", cmd.Line, cmd.Text)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if callErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("return_call_ref.wast:%d result = %v, err=%v, want %v", cmd.Line, got, callErr, cmd.Expected)
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
				t.Errorf("return_call_ref.wast:%d result = %v, want %v", cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("return_call_ref.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 || counts.Failures != 0 {
		t.Fatalf("staged return_call_ref has hidden gaps: %+v", counts)
	}
	delta := stagedTypedTailDelta{Schema: 1, SuiteRevision: stagedRelease3Revision, File: "return_call_ref", Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedTypedTailDeltaPath)
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
		t.Fatalf("staged return_call_ref delta changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact command accounting\n%s", got)
	}
	t.Logf("staged return_call_ref: commands=%d modules=%d assertions=%d expected-invalid=%d",
		counts.Commands, counts.ModulesPassed, counts.AssertionsPassed, counts.ExpectedInvalid)
}

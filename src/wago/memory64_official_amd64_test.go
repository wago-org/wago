//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stagedMemory64DeltaPath = "tests/spec-v3-staged-memory64.json"

var stagedMemory64OfficialFiles = []string{
	"address64", "align64", "float_memory64", "load64", "memory_grow64", "memory_trap64",
}

type stagedMemory64Delta struct {
	Schema        int                   `json:"schema"`
	SuiteRevision string                `json:"suite_revision"`
	Files         []stagedSpecFileDelta `json:"files"`
	Totals        stagedSpecCounts      `json:"totals"`
}

func stagedMemory64KnownGate(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	for _, gate := range []string{
		"memory64 requires an explicit bounded maximum",
		"exceeds staged ceiling 65535",
		"requires an explicit maximum no greater than 65535 pages",
		"outside staged scalar family",
		"requires exactly one local memory",
	} {
		if strings.Contains(text, gate) {
			return true
		}
	}
	return false
}

func replayStagedMemory64Script(t *testing.T, base, tmp string, script stagedSpecScript) (counts stagedSpecCounts) {
	t.Helper()
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
	instantiate := func(data []byte) (*Instance, error) {
		c, err := compileStagedMemory64(data)
		if err != nil {
			return nil, err
		}
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			_ = c.Close()
			return nil, err
		}
		compiled = append(compiled, c)
		live = append(live, in)
		return in, nil
	}
	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module: %v", base, cmd.Line, err)
				current = nil
				continue
			}
			in, err := instantiate(data)
			if err != nil {
				if stagedMemory64KnownGate(err) {
					counts.ExpectedFeatureRejects++
					current = nil
					continue
				}
				counts.UnexpectedCompileRejects++
				counts.Failures++
				t.Errorf("%s.wast:%d module rejected: %v", base, cmd.Line, err)
				current = nil
				continue
			}
			current = in
			counts.ModulesPassed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read invalid module: %v", base, cmd.Line, err)
				continue
			}
			if c, err := compileStagedMemory64(data); err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("%s.wast:%d invalid module compiled: %s", base, cmd.Line, cmd.Text)
			} else {
				counts.ExpectedInvalid++
			}
		case "assert_malformed":
			counts.ExpectedMalformed++
		case "assert_unlinkable", "assert_uninstantiable":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read rejected module: %v", base, cmd.Line, err)
				continue
			}
			in, err := instantiate(data)
			if err == nil {
				_ = in.Close()
				counts.Failures++
				t.Errorf("%s.wast:%d expected instantiation rejection: %s", base, cmd.Line, cmd.Text)
				continue
			}
			if cmd.Type == "assert_unlinkable" {
				counts.ExpectedUnlinkable++
			} else {
				counts.ExpectedUninstantiable++
			}
		case "assert_return", "action", "assert_trap":
			if current == nil {
				counts.BlockedCommands++
				continue
			}
			if cmd.Action.Type != "invoke" {
				counts.Failures++
				t.Errorf("%s.wast:%d unsupported action %q", base, cmd.Line, cmd.Action.Type)
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
				t.Errorf("%s.wast:%d unsupported argument", base, cmd.Line)
				continue
			}
			got, callErr := current.Invoke(cmd.Action.Field, args...)
			if cmd.Type == "assert_trap" {
				if callErr == nil {
					counts.Failures++
					t.Errorf("%s.wast:%d expected trap: %s", base, cmd.Line, cmd.Text)
				} else {
					counts.AssertionsPassed++
				}
				continue
			}
			if callErr != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("%s.wast:%d result=%v err=%v want=%v", base, cmd.Line, got, callErr, cmd.Expected)
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
				t.Errorf("%s.wast:%d result=%v want=%v", base, cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("%s.wast:%d unhandled command %q", base, cmd.Line, cmd.Type)
		}
	}
	return counts
}

func TestStagedOfficialMemory64ScalarAccounting(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	delta := stagedMemory64Delta{Schema: 1, SuiteRevision: stagedRelease3Revision}
	for _, base := range stagedMemory64OfficialFiles {
		t.Run(base, func(t *testing.T) {
			var script stagedSpecScript
			tmp := stagedOfficialCoreJSON(t, "memory64", base, &script)
			counts := replayStagedMemory64Script(t, base, tmp, script)
			delta.Files = append(delta.Files, stagedSpecFileDelta{Name: "memory64/" + base, Status: "accounted", Counts: counts})
			delta.Totals.add(counts)
		})
	}
	if delta.Totals.UnexpectedCompileRejects != 0 || delta.Totals.UnexpectedLinkRejects != 0 || delta.Totals.Failures != 0 {
		t.Fatalf("staged memory64 accounting has hidden gaps: %+v", delta.Totals)
	}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedMemory64DeltaPath)
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
		t.Fatalf("staged memory64 accounting changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact gates\n%s", got)
	}
	t.Logf("staged memory64 accounting: files=%d commands=%d modules=%d assertions=%d feature-rejects=%d blocked=%d invalid=%d malformed=%d",
		len(delta.Files), delta.Totals.Commands, delta.Totals.ModulesPassed, delta.Totals.AssertionsPassed,
		delta.Totals.ExpectedFeatureRejects, delta.Totals.BlockedCommands, delta.Totals.ExpectedInvalid, delta.Totals.ExpectedMalformed)
}

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

const stagedIndirectTailDeltaPath = "tests/spec-v3-staged-return-call-indirect.json"

var stagedIndirectTailBlockedLines = map[int]bool{
	239: true, 240: true, 241: true, 242: true, 244: true,
	246: true, 247: true, 248: true, 249: true, 251: true,
	252: true, 253: true, 254: true, 256: true, 257: true,
	258: true, 259: true, 260: true, 261: true, 262: true,
	263: true, 264: true, 266: true, 267: true, 268: true,
	269: true, 270: true, 271: true, 273: true, 274: true,
	275: true, 277: true, 278: true, 279: true, 280: true,
	282: true, 283: true, 284: true, 285: true, 286: true,
	287: true, 288: true, 289: true, 290: true, 291: true,
	292: true, 293: true, 295: true, 296: true,
}

func TestStagedOfficialReturnCallIndirect(t *testing.T) {
	if _, err := exec.LookPath("wast2json"); err != nil {
		t.Skip("wast2json (WABT 1.0.41) not on PATH")
	}
	var script stagedSpecScript
	tmp := stagedOfficialCoreJSON(t, "", "return_call_indirect", &script)
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
				t.Errorf("return_call_indirect.wast:%d read module: %v", cmd.Line, err)
				current = nil
				continue
			}
			c, err := compileStagedTail(data)
			if err != nil {
				if cmd.Line == 3 && strings.Contains(err.Error(), "private immutable local funcref table") {
					counts.ExpectedFeatureRejects++
					current = nil
					continue
				}
				counts.UnexpectedCompileRejects++
				counts.Failures++
				t.Errorf("return_call_indirect.wast:%d compile: %v", cmd.Line, err)
				current = nil
				continue
			}
			in, err := instantiateCore(c, InstantiateOptions{Imports: standard})
			if err != nil {
				_ = c.Close()
				counts.UnexpectedLinkRejects++
				counts.Failures++
				t.Errorf("return_call_indirect.wast:%d instantiate: %v", cmd.Line, err)
				current = nil
				continue
			}
			compiled = append(compiled, c)
			live = append(live, in)
			current = in
			counts.ModulesPassed++
		case "assert_malformed":
			counts.ExpectedMalformed++
		case "assert_invalid":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("return_call_indirect.wast:%d read invalid module: %v", cmd.Line, err)
				continue
			}
			c, err := compileStagedTail(data)
			if err == nil {
				_ = c.Close()
				counts.Failures++
				t.Errorf("return_call_indirect.wast:%d invalid module compiled: %s", cmd.Line, cmd.Text)
				continue
			}
			counts.ExpectedInvalid++
		case "assert_return", "assert_trap":
			if current == nil {
				if stagedIndirectTailBlockedLines[cmd.Line] {
					counts.BlockedCommands++
					continue
				}
				counts.Failures++
				t.Errorf("return_call_indirect.wast:%d action has no live module", cmd.Line)
				continue
			}
			counts.Failures++
			t.Errorf("return_call_indirect.wast:%d unexpectedly reached action while family gate remains", cmd.Line)
		default:
			counts.Failures++
			t.Errorf("return_call_indirect.wast:%d unhandled command %q", cmd.Line, cmd.Type)
		}
	}
	if counts.UnexpectedCompileRejects != 0 || counts.UnexpectedLinkRejects != 0 || counts.Failures != 0 {
		t.Fatalf("staged return_call_indirect has hidden gaps: %+v", counts)
	}
	delta := stagedTailFileDelta{Schema: 1, SuiteRevision: stagedRelease3Revision, File: "return_call_indirect", Counts: counts}
	got, err := json.MarshalIndent(delta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path, err := resolveRepoPath(stagedIndirectTailDeltaPath)
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
		t.Fatalf("staged return_call_indirect delta changed; rerun with WAGO_UPDATE_STAGED_SPEC=1 after reviewing exact command accounting\n%s", got)
	}
	t.Logf("staged return_call_indirect: commands=%d modules=%d invalid=%d malformed=%d feature-rejects=%d blocked=%d",
		counts.Commands, counts.ModulesPassed, counts.ExpectedInvalid, counts.ExpectedMalformed, counts.ExpectedFeatureRejects, counts.BlockedCommands)
}

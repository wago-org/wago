//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/simdfixture"
)

const rv64CallSpecMaxArtifactBytes = 16 << 20

type rv64CallSpecArtifactBinding struct {
	SourceSHA256   string `json:"source_sha256"`
	FixturesSHA256 string `json:"fixtures_sha256"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

// Prepare with an ordinary build, then load with an actual wago_precompiled
// build. Only native module execution applies in the latter: its explicit
// source_validation_required counters do not claim to validate source binaries.
// The full source/roundtrip suites remain required qualification separately.
func TestRV64PublicPinnedCallSpecPreparedArtifacts(t *testing.T) {
	mode := os.Getenv("WAGO_RV64_CALL_STAGE_ARTIFACT_MODE")
	if mode == "" {
		t.Skip("set WAGO_RV64_CALL_STAGE_ARTIFACT_MODE=prepare or load and WAGO_RV64_CALL_STAGE_ARTIFACT_DIR")
	}
	if mode != "prepare" && mode != "load" {
		t.Fatal("unknown call artifact mode", mode)
	}
	if mode == "prepare" && !rv64CallSpecCompilerPresent || mode == "load" && rv64CallSpecCompilerPresent {
		t.Fatal("prepare requires a compiler build; load requires the wago_precompiled build tag")
	}
	artifactDir := os.Getenv("WAGO_RV64_CALL_STAGE_ARTIFACT_DIR")
	if artifactDir == "" {
		t.Fatal("call artifact directory missing")
	}
	for _, profile := range []struct {
		name, variable, filesHash string
		policy                    rv64PublicSpecPolicy
		commands                  int
	}{
		{"fac", "WAGO_RV64_FAC_SPEC_JSON_DIR", rv64FacSpecFilesSHA256, rv64PublicFacSpecPolicy(), 8},
		{"call", "WAGO_RV64_CALL_SPEC_JSON_DIR", rv64CallSpecFilesSHA256, rv64PublicCallSpecPolicy(), 91},
	} {
		t.Run(profile.name, func(t *testing.T) {
			dir := os.Getenv(profile.variable)
			if dir == "" {
				t.Fatal("pinned fixture directory missing", profile.variable)
			}
			manifest, err := rv64PublicNumericManifest(dir, profile.name)
			if err != nil {
				t.Fatal(err)
			}
			data, err := manifest.Read(dir, profile.name+".json", 16<<20)
			if err != nil {
				t.Fatal(err)
			}
			var document simdfixture.Document
			if err := simdfixture.Decode(data, &document); err != nil || document.SourceFilename != profile.name+".wast" || len(document.Commands) != profile.commands {
				t.Fatal("unexpected pinned call document", err)
			}
			var in *rv64PublicSpecInstance
			defer func() {
				if err := in.close(); err != nil {
					t.Error(err)
				}
			}()
			counts := map[string]int{}
			for _, command := range document.Commands {
				switch command.Type {
				case "module":
					if in != nil || command.Filename != profile.name+".0.wasm" {
						t.Fatal("unexpected call module or module order", command.Filename)
					}
					source, err := manifest.Read(dir, command.Filename, railshot.MaxModuleBytes)
					if err != nil {
						t.Fatal(err)
					}
					blob := rv64PublicCallSpecArtifact(t, mode, artifactDir, profile.name, source, profile.filesHash, profile.policy)
					loaded, err := LoadTrustedArtifact(blob)
					in = &rv64PublicSpecInstance{compiled: loaded}
					if err != nil {
						t.Fatal("load prepared call artifact", err)
					}
					rv64PublicSpecPendingArtifact(t, loaded)
					if err := in.activate(t, nil, profile.policy); err != nil {
						t.Fatal("activate prepared call artifact", err)
					}
					counts["modules"]++
				case "assert_return", "assert_trap", "assert_exhaustion":
					if in == nil {
						t.Fatal("call assertion before module")
					}
					in.invoke(t, command, coreruntime.TrapDivOverflow)
					counts[command.Type]++
				case "assert_invalid":
					if profile.name != "call" || command.ModuleType != "binary" {
						t.Fatal("unexpected source-only applicability", command)
					}
					if _, err := manifest.Read(dir, command.Filename, railshot.MaxModuleBytes); err != nil {
						t.Fatal(err)
					}
					switch command.Text {
					case "type mismatch":
						counts["source_type_mismatch_required"]++
					case "unknown function":
						counts["source_unknown_function_required"]++
					default:
						t.Fatal("unaccounted source-only diagnostic", command.Text)
					}
					counts["source_validation_required"]++
				default:
					t.Fatal("unexpected pinned call command", command.Type)
				}
			}
			want := rv64PublicCallSpecWant(profile.name)
			if profile.name == "call" {
				delete(want, "assert_invalid")
				delete(want, "assert_invalid_validate")
				want["source_validation_required"] = 18
				want["source_type_mismatch_required"] = 16
				want["source_unknown_function_required"] = 2
			}
			t.Logf("pinned %s artifact %s execution/applicability: %v", profile.name, mode, counts)
			if !reflect.DeepEqual(counts, want) {
				t.Fatal("prepared call artifact coverage changed", counts, "want", want)
			}
		})
	}
}

func rv64PublicCallSpecArtifact(t *testing.T, mode, dir, profile string, source []byte, fixturesHash string, policy rv64PublicSpecPolicy) []byte {
	t.Helper()
	sourceHash := sha256.Sum256(source)
	binding := rv64CallSpecArtifactBinding{SourceSHA256: hex.EncodeToString(sourceHash[:]), FixturesSHA256: fixturesHash}
	if mode == "prepare" {
		compiled, err := Compile(policy.config(), source)
		if err != nil {
			t.Fatal("prepare call artifact", err)
		}
		defer func() {
			if err := compiled.Close(); err != nil {
				t.Error(err)
			}
		}()
		if compiled.codeCache.base != 0 || compiled.requiredAMD64Features != 0 {
			t.Fatal("call artifact preparation published code or selected foreign ISA")
		}
		blob, err := compiled.MarshalBinary()
		if err != nil || len(blob) > rv64CallSpecMaxArtifactBytes {
			t.Fatal("marshal bounded call artifact", err)
		}
		hash := sha256.Sum256(blob)
		binding.ArtifactSHA256 = hex.EncodeToString(hash[:])
		metadata, err := json.Marshal(binding)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		rv64PublicCallSpecWriteExclusive(t, filepath.Join(dir, profile+".wago"), blob)
		rv64PublicCallSpecWriteExclusive(t, filepath.Join(dir, profile+".wago.json"), metadata)
		return blob
	}
	metadata, err := simdfixture.Read(dir, profile+".wago.json", 4096)
	if err != nil {
		t.Fatal(err)
	}
	var recorded rv64CallSpecArtifactBinding
	if err := simdfixture.Decode(metadata, &recorded); err != nil {
		t.Fatal(err)
	}
	blob, err := simdfixture.Read(dir, profile+".wago", rv64CallSpecMaxArtifactBytes)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(blob)
	binding.ArtifactSHA256 = hex.EncodeToString(hash[:])
	if recorded != binding {
		t.Fatal("prepared call artifact source/fixture/digest binding differs")
	}
	return blob
}

func rv64PublicCallSpecWriteExclusive(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	n, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || n != len(data) {
		t.Fatal("write prepared call artifact", writeErr, closeErr, n)
	}
}

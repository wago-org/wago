//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && wago_profile

package wago

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// This file deliberately remains in a wago_precompiled test binary. The shared
// table-stage helper prepares .wago files in a compiler-enabled process, then
// loads those exact files when WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE=load.
func TestRV64PublicProfileCompilerFreeArtifactAttachment(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			typ, input := wasm.I64, []uint64{0xffedcba987654321}
			if vector {
				typ, input = wasm.V128, []uint64{0xffedcba987654321, 0x123456789abcdef0}
			}
			compiled := rv64TableStageCompile(t, fmt.Sprintf("profile-identity-vector%t", vector), rv64PublicNativeIdentityModule([]wasm.ValType{typ}), true)
			code := slices.Clone(compiled.code)
			session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: true})
			defer session.Close()
			if err := compiled.AttachCodeProfile(session); err != nil {
				t.Fatal(err)
			}
			first, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			last, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer last.Close()
			events, status := session.Read(0)
			if len(events) != 1 || status.Dropped != 0 {
				t.Fatal("artifact mapping load was not shared", events, status)
			}
			image := events[0].Image
			if err := jitprofile.ValidateRegions(image.Regions, image.Size); err != nil {
				t.Fatal(err)
			}
			if len(image.Regions) != 1 || image.Regions[0].Kind != "unknown" || image.Regions[0].Function != -1 || len(image.Sources) != 0 || len(image.Functions) != 0 || len(image.CodeSites) != 0 || len(image.Unwind) != 0 || len(image.InlineFrames) != 0 || image.SourceCoverage != "" || image.SiteCoverage != "" || image.UnwindCoverage != "" || image.ModuleID != "" || image.Size != uint64(len(code)) || !bytes.Equal(image.Code, code) {
				t.Fatal("artifact fabricated provenance or changed image", image)
			}
			compiled.Close()
			first.Close()
			if events, _ := session.Read(0); len(events) != 1 {
				t.Fatal("artifact mapping retired before final reference", events)
			}
			got, err := last.Invoke("identity", input...)
			if err != nil || !slices.Equal(got, input) {
				t.Fatal("compiler-free profiled artifact execution", got, err)
			}
			last.Close()
			if events, status := session.Read(0); len(events) != 2 || events[1].Kind != "retire" || events[1].ImageID != image.ID || status.Dropped != 0 || !bytes.Equal(events[0].Image.Code, code) {
				t.Fatal("artifact generation lifecycle", events, status)
			}
		})
	}
}

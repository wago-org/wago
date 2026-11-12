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

func TestRV64PublicProfileSourcesAndMappingLifetime(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, sources := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-sources%t", vector, sources), func(t *testing.T) {
				typ, input := wasm.I64, []uint64{0xffedcba987654321}
				if vector {
					typ, input = wasm.V128, []uint64{0xffedcba987654321, 0x123456789abcdef0}
				}
				data := rv64PublicNativeIdentityModule([]wasm.ValType{typ})
				plain, err := Compile(data)
				if err != nil {
					t.Fatal(err)
				}
				defer plain.Close()
				session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: sources})
				defer session.Close()
				compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), data)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				if !bytes.Equal(plain.code, compiled.code) {
					t.Fatal("profiling changed executable bytes")
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
					t.Fatal("mapping was not shared", len(events), status)
				}
				image := events[0].Image
				if err := jitprofile.ValidateRegions(image.Regions, image.Size); err != nil {
					t.Fatal(err)
				}
				if err := jitprofile.ValidateSources(image.Sources, image.Size); err != nil {
					t.Fatal(err)
				}
				if image.Size != uint64(len(compiled.code)) || !bytes.Equal(image.Code, compiled.code) || len(image.Functions) != 1 || image.Functions[0].Index != 0 || image.Functions[0].NativeBytes != len(compiled.code) {
					t.Fatal("profile does not describe exact finalized image", image)
				}
				if len(image.Unwind) != 0 || len(image.InlineFrames) != 0 || image.UnwindCoverage != "" {
					t.Fatal("unqualified diagnostic attribution", image)
				}
				if sources {
					if len(image.CodeSites) == 0 || image.SiteCoverage != "frame-loads-stores-and-return-address-transfers" {
						t.Fatal("missing real frame sites", image)
					}
					if len(image.Sources) == 0 || image.SourceCoverage != "sparse-opcode-and-deferred-origins-with-wrapper-helper-gaps" {
						t.Fatal("missing truthful source coverage", image)
					}
				} else if len(image.Sources) != 0 || len(image.CodeSites) != 0 || image.SiteCoverage != "" || image.SourceCoverage != "" {
					t.Fatal("unrequested source map", image)
				}
				compiled.Close()
				first.Close()
				if events, _ := session.Read(0); len(events) != 1 {
					t.Fatal("mapping retired before last owner", events)
				}
				got, err := last.Invoke("identity", input...)
				if err != nil || !slices.Equal(got, input) {
					t.Fatal("profiled invocation after compiler close", got, err)
				}
				last.Close()
				if events, _ := session.Read(0); len(events) != 2 || events[1].Kind != "retire" || !bytes.Equal(events[0].Image.Code, plain.code) {
					t.Fatal("mapping retirement or retained bytes", events)
				}
			})
		}
	}
}

func TestRV64PublicProfileArtifactHasNoInventedSources(t *testing.T) {
	sourceSession := NewCodeProfile(CodeProfileOptions{SourceMaps: true})
	defer sourceSession.Close()
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithCodeProfile(sourceSession), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: true})
	defer session.Close()
	if err := compiled.AttachCodeProfile(session); err != nil {
		t.Fatal(err)
	}
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	images, _, status := session.Snapshot()
	if len(images) != 1 || status.Dropped != 0 {
		t.Fatal(images, status)
	}
	image := images[0]
	if len(image.Regions) != 1 || image.Regions[0].Kind != "unknown" || len(image.Sources) != 0 || len(image.Functions) != 0 || len(image.Unwind) != 0 || image.SourceCoverage != "" || image.UnwindCoverage != "" || !bytes.Equal(image.Code, compiled.code) {
		t.Fatal("artifact invented compiler provenance", image)
	}
	if got, err := instance.Invoke("identity", 41); err != nil || len(got) != 1 || got[0] != 41 {
		t.Fatal(got, err)
	}
}

func TestRV64PublicProfileUnwindPublishedFromCode(t *testing.T) {
	session := NewCodeProfile(CodeProfileOptions{SourceMaps: true, UnwindMaps: true})
	defer session.Close()
	compiled, err := Compile(NewRuntimeConfig().WithCodeProfile(session), identityI32Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	events, status := session.Read(0)
	if len(events) != 1 || status.Dropped != 0 {
		t.Fatal("profile mapping", len(events), status)
	}
	image := events[0].Image
	if len(image.Unwind) == 0 || image.UnwindCoverage != "rv64-proved-stack-return-addresses" || len(image.CodeSites) == 0 {
		t.Fatal("missing real RV64 instruction metadata", image)
	}
	if err := jitprofile.ValidateUnwind(image.Unwind, image.Size); err != nil {
		t.Fatal(err)
	}
	if err := jitprofile.ValidateCodeSiteRegions(image.CodeSites, image.Regions); err != nil {
		t.Fatal(err)
	}
	if got, err := in.Invoke("identity", 42); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatal(got, err)
	}
}

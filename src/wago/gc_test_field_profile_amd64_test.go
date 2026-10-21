//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_profile

package wago

import (
	"bytes"
	"os"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCTestFieldProfile(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-test-field.wasm")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: true, UnwindMaps: true})
	defer session.Close()
	observed, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCodeProfile(session), data)
	if err != nil {
		t.Fatal(err)
	}
	defer observed.Close()
	plainInstance, err := Instantiate(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer plainInstance.Close()
	in, err := Instantiate(observed, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !bytes.Equal(plain.code, observed.code) {
		t.Fatal("profiling changed fused native code")
	}
	events, status := session.Read(0)
	if status.Dropped != 0 || len(events) != 1 {
		t.Fatalf("profile images=%d dropped=%d", len(events), status.Dropped)
	}
	image := events[0].Image
	if err = jitprofile.ValidateSources(image.Sources, image.Size); err != nil {
		t.Fatal(err)
	}
	if err = jitprofile.ValidateInlineSources(image.Sources, image.InlineFrames); err != nil {
		t.Fatal(err)
	}
	if err = jitprofile.ValidateUnwind(image.Unwind, image.Size); err != nil {
		t.Fatal(err)
	}
	module, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	index := plain.Exports["read"]
	body := module.Code[index-plain.NumImports]
	field := bytes.Index(body.BodyBytes, []byte{0xfb, 2, 0, 0})
	if field < 0 {
		t.Fatal("read fixture lost first field read")
	}
	pc := body.LocalDeclBytes + uint32(field)
	found := false
	for _, source := range image.Sources {
		if source.Function == uint32(index) && source.WasmOffset == pc {
			found = true
		}
	}
	if !found {
		t.Fatalf("fused first field lacks source range at func%d PC%d", index, pc)
	}

	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	if got, e := in.Invoke("stack-root"); e != nil || len(got) != 1 || got[0] != 17 {
		t.Fatalf("profiled older root=%v,%v", got, e)
	}
}

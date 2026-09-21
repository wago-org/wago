//go:build arm64 && !tinygo

package wago

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// TestARM64DraglineDirectCallResultSurvivesFollowingCall covers a minimized
// json-as startup sequence. Register pressure assigns the first direct call's
// i32 result to a spill slot, and a second direct call follows before the
// result is consumed as a memory address.
func TestARM64DraglineDirectCallResultSurvivesFollowingCall(t *testing.T) {
	const encoded = "AGFzbQEAAAABFQRgAABgAn9/AX9gAn9/AGADf39+AAMIBwECAAMAAAAFAwEAAQYVBH8BQQALfwFBAAt/AUEAC38BQQALBwoBBl9zdGFydAAGCAEFCqQCBwQAQQELKQEEf0EABH9BACICGkEAIgMaQQEFIAQLIAE2AgAgAhogAxogACAFahoLDgBBAEEAQgAQA0EAJAALCABBAEEAEAELBAAQAguOAQEEf0EAJAEBAQEBAQEBAQEBASMDJAMBAQEBAQEBAQEBAQEBAQEjAyQDAQFBACQDAQEBAQEBQQAkAQEBAQEBQQAkAwEBAQEBQQAkAwFBACQBAQEBAQEBQQBBAWokAQEBAQEBQQAkAQEBQQAkAUEAJANBAEEAEAAiAxoBEAQBQQBBAWokAUEAJAEBIAMkAgtGAQN/A0AgAEUEQCMCIgEaAQEBAQEBASABQQFrKAIQGgEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAUEBIQAMAQsLCw=="
	module, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("_start"); err != nil {
		t.Fatal(err)
	}
}

// TestARM64DraglineJSONASStartup verifies that a structured caller refreshes
// its cached linear-memory bound after a direct serializer callee grows memory,
// even when the callee's private ABI preserves pinned registers.
func TestARM64DraglineJSONASStartup(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("..", "..", "bench", "startup", "twins", "json-as.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("_start"); err != nil {
		t.Fatal(err)
	}
}

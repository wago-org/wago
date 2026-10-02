//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"errors"
	"strings"
	"testing"
)

func TestCompilerEngineConfiguration(t *testing.T) {
	base := NewRuntimeConfig()
	if got := base.Compiler(); got != CompilerRailshot {
		t.Fatalf("default compiler = %v, want railshot", got)
	}
	dragline := base.WithCompiler(CompilerDragline)
	if got := dragline.Compiler(); got != CompilerDragline {
		t.Fatalf("selected compiler = %v, want dragline", got)
	}
	if base.Compiler() != CompilerRailshot {
		t.Fatal("WithCompiler mutated the base configuration")
	}
	if err := base.WithCompiler(CompilerEngine(255)).Validate(); err == nil || !strings.Contains(err.Error(), "unknown compiler engine") {
		t.Fatalf("invalid compiler validation = %v", err)
	}
}

func TestStrictEmptyDraglineDoesNotFallBack(t *testing.T) {
	_, err := NewRuntimeConfig().WithCompiler(CompilerDragline).Compile(signExtModule())
	var unavailable *DraglineUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Dragline compile = %v, want strict unavailable error", err)
	}
}

func TestCompiledArtifactPreservesCompilerEngine(t *testing.T) {
	c, err := NewRuntimeConfig().Compile(signExtModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Compiler() != CompilerRailshot {
		t.Fatalf("fresh artifact compiler = %v", c.Compiler())
	}
	c.compiler = CompilerDragline // exercise codec identity before Dragline emits code
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.Compiler() != CompilerDragline {
		t.Fatalf("loaded artifact compiler = %v, want dragline", loaded.Compiler())
	}
}

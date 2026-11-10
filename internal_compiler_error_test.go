package wago_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago"
)

func TestPublicInternalCompilerErrorClassification(t *testing.T) {
	cause := errors.New("backend invariant")
	original := &wago.InternalCompilerError{Backend: "amd64", FunctionIndex: 2, WasmOffset: 4, Panic: cause}
	var ice *wago.InternalCompilerError
	if !errors.As(fmt.Errorf("compile: %w", original), &ice) || ice != original || !errors.Is(ice, cause) {
		t.Fatal("public facade lost typed error or original cause")
	}
	_, err := wago.Compile(nil, []byte("not Wasm"))
	if err == nil || errors.As(err, &ice) {
		t.Fatalf("ordinary decode rejection misclassified: %v", err)
	}
}

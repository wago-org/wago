package railshot

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestInternalCompilerErrorPreservesContextAndCause(t *testing.T) {
	cause := errors.New("test encoder invariant")
	ice := NewInternalCompilerError("amd64", 7, 23, cause)
	var got *InternalCompilerError
	if !errors.As(fmt.Errorf("compile: %w", ice), &got) || got != ice || !errors.Is(ice, cause) {
		t.Fatalf("wrapped classification/cause lost: %v", ice)
	}
	if ice.FunctionIndex != 7 || ice.WasmOffset != 23 || ice.Panic != cause || !strings.Contains(ice.Error(), "wasm offset 0x17") {
		t.Fatalf("context lost: %+v", ice)
	}
	if len(ice.Stack) > 16<<10 {
		t.Fatalf("unbounded stack: %d", len(ice.Stack))
	}
	if next := NewInternalCompilerError("arm64", 99, 99, ice); next != ice {
		t.Fatal("nested recovery replaced the original context")
	}
	if NewInternalCompilerError("arm64", 0, -1, "panic value").Unwrap() != nil {
		t.Fatal("non-error panic unexpectedly unwraps")
	}
}

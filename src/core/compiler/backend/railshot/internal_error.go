package railshot

import "fmt"

// InternalCompilerError distinguishes a recovered code-generation panic from
// unsupported or invalid Wasm. The compile entry point may wrap this error.
type InternalCompilerError struct {
	Backend string
	// FunctionIndex is the absolute Wasm function index, including imports.
	FunctionIndex int
	// WasmOffset is the current function-body byte offset, including local
	// declarations, or -1 when no instruction location is available.
	WasmOffset int
	// Panic is the original recovered value. Error values remain unwrap-able.
	Panic any
	// Stack is a bounded current-goroutine snapshot (at most 16 KiB), possibly
	// truncated. It is absent in TinyGo builds to preserve their runtime footprint.
	Stack []byte
}

func (e *InternalCompilerError) Error() string {
	if e.WasmOffset >= 0 {
		return fmt.Sprintf("%s: internal compiler error in function %d at wasm offset %#x: %v", e.Backend, e.FunctionIndex, e.WasmOffset, e.Panic)
	}
	return fmt.Sprintf("%s: internal compiler error in function %d: %v", e.Backend, e.FunctionIndex, e.Panic)
}

func (e *InternalCompilerError) Unwrap() error {
	cause, _ := e.Panic.(error)
	return cause
}

// NewInternalCompilerError captures diagnostics only on the recovered-panic path.
func NewInternalCompilerError(backend string, function, offset int, value any) *InternalCompilerError {
	if previous, ok := value.(*InternalCompilerError); ok && previous != nil {
		return previous
	}
	return &InternalCompilerError{Backend: backend, FunctionIndex: function, WasmOffset: offset, Panic: value, Stack: compilerPanicStack()}
}

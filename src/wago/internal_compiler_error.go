package wago

import railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot"

// InternalCompilerError identifies a recovered native code-generator panic.
// Use errors.As on a compilation error to distinguish it from rejected Wasm.
// It includes backend, function, bytecode context, the original panic, and a
// bounded Go stack snapshot (the snapshot is omitted in TinyGo builds).
type InternalCompilerError = railshot.InternalCompilerError

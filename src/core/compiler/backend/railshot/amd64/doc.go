//go:build amd64

// Package amd64 holds the x86-64-specific half of the railshot single-pass
// backend: the condense engine / instruction selection, linear-memory + bounds
// lowering, scalar-float (SSE) and v128 (SSE/AVX) lowering, the calling
// convention, table/global ops, and magic-division lowering — everything that
// drives the x86-64 instruction encoder (src/core/encoder/amd64).
//
// The architecture-NEUTRAL core — the valent-block operand stack, the on-the-fly
// register allocator, the scanBody hint pre-scan, and the control-flow
// reconciliation model — lives in the parent railshot package and is shared with
// the AArch64 twin in src/core/compiler/backend/railshot/arm64.
//
// Migration in progress: the arch-specific files are being moved here out of the
// parent package (which becomes neutral-only). See docs/arm64-port plan.
package amd64

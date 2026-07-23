// Package ir contains Wago's off-path scalar SSA research and verification
// representation. It is not an execution tier: production decode, validation,
// compilation, instantiation, and execution must not import this package.
//
// Keep the package bounded to concrete oracle, debugging, or shape-verification
// uses. Do not expand its feature surface merely to mirror production Railshot.
package ir

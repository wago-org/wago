// Package runtime implements commands backed by a compiled Wago engine.
//
// It contains no release networking or toolchain management. The manager
// selects the runtime artifact, including any compiled plugins, before launch.
package runtime

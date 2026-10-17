//go:build tinygo

package wago

// TinyGo's task stack is fixed and smaller than a standard Go goroutine stack.
// Reject the fourth nested re-entry before a saved outer caller plus the native
// boundary frames can reach its task-stack canary.
const maxHostReentryDepth = 3

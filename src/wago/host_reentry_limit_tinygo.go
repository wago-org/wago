//go:build tinygo

package wago

// TinyGo's fixed task stack needs a smaller bound. Reject the fourth nested
// reentry before saved callers and native boundary frames reach the canary.
const maxHostReentryDepth = 3

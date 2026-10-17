//go:build tinygo

package wago

// Keep the RV64 PR's conservative task-stack bound. Reject the fourth nested
// reentry before saved callers and native boundary frames reach the canary.
const maxHostReentryDepth = 3

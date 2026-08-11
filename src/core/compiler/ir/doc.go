// Package ir contains Wago's bounded block/value representation. Railshot's
// direct one-pass compiler remains the general execution tier; selected large,
// straight-line functions may use the per-function builder for optimizations
// that require cross-statement dependence information.
package ir

//go:build amd64 && tinygo && wago_lean

package amd64

// Keep ordinary GC lowering in the lean TinyGo profile. Complete-loop proof
// recognizers are optional compiler work and exceed this profile's size budget.
const gcInductionReductionsEnabled = false

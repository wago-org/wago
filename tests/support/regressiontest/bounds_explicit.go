//go:build !wago_guardpage && !(darwin && arm64 && !tinygo && !wago_target_tinygo)

package regressiontest

const ExpectedBounds = "explicit"

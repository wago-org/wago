//go:build !tinygo || !wago_minimal

package wago

const compiledAVXFeatureMask = compiledCPUFeatureAVX2 | compiledCPUFeatureAVX512

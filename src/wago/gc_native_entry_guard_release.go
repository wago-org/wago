//go:build !wagodebug

package wago

func validateNativeGCEntry(*Instance) error { return nil }

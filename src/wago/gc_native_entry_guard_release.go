//go:build !wagodebug

package wago

const nativeGCEntryValidationEnabled = false

func validateNativeGCEntry(*Instance) error { return nil }

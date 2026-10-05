//go:build !riscv64

package wago

func railshotAdmitSource([]byte) error         { return nil }
func railshotValidateCompiled(*Compiled) error { return nil }

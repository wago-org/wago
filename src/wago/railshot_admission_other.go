//go:build !riscv64

package wago

func railshotAdmitSource([]byte) error                                 { return nil }
func railshotValidateCompiled(*Compiled) error                         { return nil }
func railshotValidateResolvedImports(*Compiled, resolvedImports) error { return nil }
func railshotNeedsImportContextHeader(int) bool                        { return false }

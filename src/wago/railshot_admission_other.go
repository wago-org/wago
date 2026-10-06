//go:build !riscv64

package wago

func railshotAdmitSource([]byte) error                                                  { return nil }
func railshotValidateCompiled(*Compiled) error                                          { return nil }
func railshotValidateResolvedImports(*Compiled, resolvedImports, *referenceStore) error { return nil }
func railshotNeedsImportContextHeader(int) bool                                         { return false }

func railshotPublishConstructedTableProducer(*Table, *Instance) *Instance { return nil }

func railshotLockNativeTableScan()   {}
func railshotUnlockNativeTableScan() {}

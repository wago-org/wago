package wago

// exactImportModuleEnd encodes one plus the module-name byte length so zero can
// retain the legacy first-dot interpretation for hand-built Compiled values.
func exactImportModuleEnd(module string) uint64 { return uint64(len(module)) + 1 }

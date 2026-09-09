package run

import (
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/cli/internal/wasmcall"
)

func mustParseArgs(values []string, params []wago.ValType) []uint64 {
	arguments, err := wasmcall.ParseArgs(values, params)
	if err != nil {
		ui.Fatal("%v", err)
	}
	return arguments
}

func parseVal(value string, valueType wago.ValType) (uint64, error) {
	return wasmcall.ParseValue(value, valueType)
}

func fmtVal(bits uint64, valueType wago.ValType) string {
	return wasmcall.FormatValue(bits, valueType)
}

func format(results []uint64, resultTypes []wago.ValType) string {
	if len(results) == 0 {
		return ""
	}
	return wasmcall.FormatResults(results, resultTypes)
}

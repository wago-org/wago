package run

import (
	"fmt"
	"strings"

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

func format(export string, args, results []uint64, paramTypes, resultTypes []wago.ValType) string {
	arguments := make([]string, len(args))
	for index, value := range args {
		arguments[index] = fmtVal(value, paramTypes[index])
	}
	call := fmt.Sprintf("%s(%s)", export, strings.Join(arguments, ", "))
	if len(results) == 0 {
		return fmt.Sprintf("%s = %s", call, ui.Dim("()"))
	}
	formatted := make([]string, len(results))
	for index, value := range results {
		formatted[index] = fmtVal(value, resultTypes[index])
	}
	return fmt.Sprintf("%s = %s", call, ui.Cyan(strings.Join(formatted, ", ")))
}

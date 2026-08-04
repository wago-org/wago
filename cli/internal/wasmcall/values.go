// Package wasmcall parses and formats typed WebAssembly function values.
package wasmcall

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wago-org/wago"
)

func ParseArgs(values []string, params []wago.ValType) ([]uint64, error) {
	if len(values) != len(params) {
		return nil, fmt.Errorf("expected %d arg(s), got %d", len(params), len(values))
	}
	arguments := make([]uint64, len(values))
	for index, value := range values {
		valueType := params[index]
		literal := value
		if suffix := strings.LastIndexByte(value, ':'); suffix >= 0 {
			literal = value[:suffix]
			switch value[suffix+1:] {
			case "i32":
				valueType = wago.ValI32
			case "i64":
				valueType = wago.ValI64
			case "f32":
				valueType = wago.ValF32
			case "f64":
				valueType = wago.ValF64
			default:
				return nil, fmt.Errorf("arg %d: bad type suffix in %q", index, value)
			}
		}
		parsed, err := ParseValue(literal, valueType)
		if err != nil {
			return nil, fmt.Errorf("arg %d (%q): %w", index, value, err)
		}
		arguments[index] = parsed
	}
	return arguments, nil
}

func ParseValue(value string, valueType wago.ValType) (uint64, error) {
	switch valueType {
	case wago.ValI64:
		if integer, err := strconv.ParseInt(value, 0, 64); err == nil {
			return wago.I64(integer), nil
		}
		unsigned, err := strconv.ParseUint(value, 0, 64)
		return wago.I64(int64(unsigned)), err
	case wago.ValF32:
		float, err := strconv.ParseFloat(value, 32)
		return wago.F32(float32(float)), err
	case wago.ValF64:
		float, err := strconv.ParseFloat(value, 64)
		return wago.F64(float), err
	default:
		if integer, err := strconv.ParseInt(value, 0, 32); err == nil {
			return wago.I32(int32(integer)), nil
		}
		unsigned, err := strconv.ParseUint(value, 0, 32)
		return wago.I32(int32(uint32(unsigned))), err
	}
}

func FormatValue(bits uint64, valueType wago.ValType) string {
	switch valueType {
	case wago.ValI64:
		return strconv.FormatInt(wago.AsI64(bits), 10)
	case wago.ValF32:
		return strconv.FormatFloat(float64(wago.AsF32(bits)), 'g', -1, 32)
	case wago.ValF64:
		return strconv.FormatFloat(wago.AsF64(bits), 'g', -1, 64)
	default:
		return strconv.FormatInt(int64(wago.AsI32(bits)), 10)
	}
}

func Format(export string, args, results []uint64, paramTypes, resultTypes []wago.ValType) string {
	arguments := make([]string, len(args))
	for index, value := range args {
		arguments[index] = FormatValue(value, paramTypes[index])
	}
	call := fmt.Sprintf("%s(%s)", export, strings.Join(arguments, ", "))
	if len(results) == 0 {
		return call + " = ()"
	}
	formatted := make([]string, len(results))
	for index, value := range results {
		formatted[index] = FormatValue(value, resultTypes[index])
	}
	return fmt.Sprintf("%s = %s", call, strings.Join(formatted, ", "))
}

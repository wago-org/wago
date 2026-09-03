package version

import (
	"strconv"
	"strings"
)

// Compare does a numeric dotted comparison, ignoring a leading v.
// Non-numeric components sort after numeric ones.
func Compare(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		if i >= len(as) {
			return -1
		}
		if i >= len(bs) {
			return 1
		}
		av, aNumeric := numericComponent(as[i])
		bv, bNumeric := numericComponent(bs[i])
		if aNumeric && bNumeric {
			if len(av) != len(bv) {
				return sign(len(av) - len(bv))
			}
			if comparison := strings.Compare(av, bv); comparison != 0 {
				return comparison
			}
			continue
		}
		if aNumeric != bNumeric {
			if aNumeric {
				return -1
			}
			return 1
		}
		if comparison := strings.Compare(as[i], bs[i]); comparison != 0 {
			return comparison
		}
	}
	return 0
}

func ParseNumeric(value string) (int, bool) {
	if _, ok := numericComponent(value); !ok {
		return 0, false
	}
	number, err := strconv.Atoi(value)
	return number, err == nil
}

func numericComponent(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	for len(value) > 1 && value[0] == '0' {
		value = value[1:]
	}
	return value, true
}

func sign(number int) int {
	switch {
	case number < 0:
		return -1
	case number > 0:
		return 1
	default:
		return 0
	}
}

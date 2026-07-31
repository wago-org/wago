package version

import "strings"

// Compare does a numeric dotted comparison, ignoring a leading v.
// Non-numeric components sort after numeric ones.
func Compare(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		var aNumeric, bNumeric bool
		if i < len(as) {
			av, aNumeric = ParseNumeric(as[i])
		}
		if i < len(bs) {
			bv, bNumeric = ParseNumeric(bs[i])
		}
		if aNumeric && bNumeric {
			if av != bv {
				return sign(av - bv)
			}
			continue
		}
		if comparison := strings.Compare(component(as, i), component(bs, i)); comparison != 0 {
			return comparison
		}
	}
	return 0
}

func ParseNumeric(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	number := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
		number = number*10 + int(character-'0')
	}
	return number, true
}

func component(values []string, index int) string {
	if index < len(values) {
		return values[index]
	}
	return ""
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

package profilecmd

import "flag"

// FlagSet describes the same flags consumed by Run, for CLI help and discovery.
// Callers must not rely on parsing this independent set to configure execution.
func FlagSet(name string) *flag.FlagSet {
	switch name {
	case "record", "capture":
		f, _ := recordFlags(name)
		return f
	case "top", "annotate", "diff":
		f, _ := reportFlags(name)
		return f
	case "timeline":
		f, _ := timelineFlags()
		return f
	default:
		return nil
	}
}

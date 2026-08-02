//go:build wago_gcstats

package gc

const (
	DiagnosticSpaceInvalid uint8 = iota
	DiagnosticSpaceImmediate
	DiagnosticSpaceNursery
	DiagnosticSpaceOld
	DiagnosticSpaceLarge
	DiagnosticSpaceTiny
)

// DiagnosticObjectStore reports collector placement for a prospective object
// store. It is available only to diagnostic wago_gcstats builds.
func (c *Collector) DiagnosticObjectStore(parent, child Ref) (parentSpace, childSpace uint8, parentRemembered bool) {
	if c == nil || !parent.IsObj() || !c.validObjectRef(parent) {
		return DiagnosticSpaceInvalid, diagnosticRefSpace(c, child), false
	}
	entry := c.entry(parent)
	return diagnosticSpace(entry.space), diagnosticRefSpace(c, child), entry.remembered
}

func diagnosticRefSpace(c *Collector, ref Ref) uint8 {
	if ref.IsNull() || ref.IsI31() {
		return DiagnosticSpaceImmediate
	}
	if c == nil || !ref.IsObj() || !c.validObjectRef(ref) {
		return DiagnosticSpaceInvalid
	}
	return diagnosticSpace(c.entry(ref).space)
}

func diagnosticSpace(space spaceKind) uint8 {
	switch space {
	case spaceNursery:
		return DiagnosticSpaceNursery
	case spaceOld:
		return DiagnosticSpaceOld
	case spaceLarge:
		return DiagnosticSpaceLarge
	case spaceTiny:
		return DiagnosticSpaceTiny
	default:
		return DiagnosticSpaceInvalid
	}
}

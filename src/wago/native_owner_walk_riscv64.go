//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

// Small attachment walks use their caller's inline storage. Large walks use
// an index so repeated attachments and cycles do not require quadratic scans.
func rv64AppendOwnerForWalk(owners []*Instance, index *map[*Instance]struct{}, owner *Instance) ([]*Instance, bool) {
	if *index != nil {
		if _, found := (*index)[owner]; found {
			return owners, true
		}
	} else {
		for _, prior := range owners {
			if prior == owner {
				return owners, true
			}
		}
	}
	if len(owners) == rv64MaxTableLeaseEdges+1 {
		return owners, false
	}
	if len(owners) == 32 && *index == nil {
		*index = make(map[*Instance]struct{}, len(owners)+1)
		for _, prior := range owners {
			(*index)[prior] = struct{}{}
		}
	}
	owners = append(owners, owner)
	if *index != nil {
		(*index)[owner] = struct{}{}
	}
	return owners, true
}

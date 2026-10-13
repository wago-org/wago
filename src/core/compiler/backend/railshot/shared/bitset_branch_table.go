package shared

// BitsetBranchTable admits a word-sized table with one nondefault target and
// at least three separated membership runs. Single and contiguous cases keep
// the existing dispatch. The walk uses only scalar scratch.
func BitsetBranchTable(labels []uint32, def uint32) (mask uint64, target uint32, ok bool) {
	if len(labels) < 8 || len(labels) > 64 {
		return 0, 0, false
	}
	found, inside := false, false
	runs, members := 0, 0
	for i, label := range labels {
		if label == def {
			inside = false
			continue
		}
		if found && label != target {
			return 0, 0, false
		}
		found, target = true, label
		members++
		mask |= uint64(1) << uint(i)
		if !inside {
			runs++
		}
		inside = true
	}
	if runs < 3 || members*4 < len(labels) {
		return 0, 0, false
	}
	return mask, target, true
}

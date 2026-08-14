package shared

// UnreadLocalMask returns the bounded set of locals whose value is never read.
// Local indexes at or above 64 deliberately retain the conservative fallback.
func UnreadLocalMask(nLocals int, read uint64) uint64 {
	limit := nLocals
	if limit > 64 {
		limit = 64
	}
	var mask uint64
	if limit == 64 {
		mask = ^uint64(0)
	} else if limit > 0 {
		mask = uint64(1)<<uint(limit) - 1
	}
	return mask &^ read
}

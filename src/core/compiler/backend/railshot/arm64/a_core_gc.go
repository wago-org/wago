//go:build arm64

package arm64

func (f *fn) tracksGCFrameRoots() bool {
	return f.gcFrameRoots != nil && f.gcFrameRoots.Candidate
}

func (f *fn) gcFrameLocal(index int) bool {
	if index < 0 || f.gcFrameRoots == nil {
		return false
	}
	return f.gcFrameRoots.TracksLocal(uint32(index))
}

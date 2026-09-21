package dragline

func arm64ShuffleLaneRotate(bytes [16]byte, laneBytes, rotateBytes byte) bool {
	for i, lane := range bytes {
		base := byte(i) / laneBytes * laneBytes
		if lane != base+(byte(i)%laneBytes+rotateBytes)%laneBytes {
			return false
		}
	}
	return true
}

func arm64ShuffleZip(bytes [16]byte, laneBytes byte, upper bool) bool {
	half := byte(8 / laneBytes)
	start := byte(0)
	if upper {
		start = half
	}
	for i, lane := range bytes {
		outputLane := byte(i) / laneBytes
		inputLane := start + outputLane/2
		expected := inputLane*laneBytes + byte(i)%laneBytes
		if outputLane&1 != 0 {
			expected += 16
		}
		if lane != expected {
			return false
		}
	}
	return true
}

func arm64ShuffleSpecialized(bytes [16]byte) bool {
	return arm64ShuffleLaneRotate(bytes, 4, 1) || arm64ShuffleLaneRotate(bytes, 4, 2) ||
		arm64ShuffleZip(bytes, 4, false) || arm64ShuffleZip(bytes, 4, true) ||
		arm64ShuffleZip(bytes, 8, false) || arm64ShuffleZip(bytes, 8, true)
}

package command

// SuggestChild returns one unambiguous nearby command name for a typo.
func SuggestChild(cmd *Cmd, value string) string {
	best, bestDistance, tied := "", len(value)+1, false
	for _, child := range cmd.Children {
		for _, candidate := range append([]string{child.Name}, child.Aliases...) {
			distance := editDistance(value, candidate)
			switch {
			case distance < bestDistance:
				best, bestDistance, tied = child.Name, distance, false
			case distance == bestDistance && best != child.Name:
				tied = true
			}
		}
	}
	limit := 1
	if len(value) >= 4 {
		limit = 2
	}
	if tied || bestDistance > limit {
		return ""
	}
	return best
}

func editDistance(left, right string) int {
	previous := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex := 1; leftIndex <= len(left); leftIndex++ {
		current := make([]int, len(right)+1)
		current[0] = leftIndex
		for rightIndex := 1; rightIndex <= len(right); rightIndex++ {
			replace := previous[rightIndex-1]
			if left[leftIndex-1] != right[rightIndex-1] {
				replace++
			}
			current[rightIndex] = min(previous[rightIndex]+1, current[rightIndex-1]+1, replace)
		}
		previous = current
	}
	return previous[len(right)]
}

package task

import "math"

// NormalizeProgress converts progress to a safe percentage range [0, 100].
// It also accepts fractional values in (0,1] and treats them as [0%,100%].
func NormalizeProgress(progress float64) float64 {
	if math.IsNaN(progress) || math.IsInf(progress, 0) {
		return 0
	}
	if progress > 0 && progress <= 1 {
		progress *= 100
	}
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

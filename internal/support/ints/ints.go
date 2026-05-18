package ints

// Min returns the smaller of a and b.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Max returns the larger of a and b.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Max64 returns the larger of a and b for uint64 values.
func Max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// PositiveMin returns left when it is positive and no greater than right.
// Otherwise it falls back to right.
func PositiveMin(left, right int) int {
	if left <= 0 || left > right {
		return right
	}
	return left
}

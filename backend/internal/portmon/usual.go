package portmon

// UsualSpeed picks a port's usual link speed: the speed seen in the most
// 5-minute buckets (counts: speed -> buckets), ties going to the faster speed.
// Returns 0 (unknown, so slow_link never fires) under 24 hours of history.
func UsualSpeed(counts map[int64]int, historyHours float64) int64 {
	if historyHours < 24 {
		return 0
	}
	var best int64
	bestN := 0
	for speed, n := range counts {
		if speed <= 0 {
			continue
		}
		if n > bestN || (n == bestN && speed > best) {
			best, bestN = speed, n
		}
	}
	return best
}

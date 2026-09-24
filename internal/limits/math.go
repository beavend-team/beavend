package limits

import (
	"math"
	"time"
)

// EstimateSlidingWindow evaluates whether an incoming request of given cost is admitted
// by the sliding window counter algorithm, and computes retryAfter if rejected.
//
// Mathematics:
// Window is divided into discrete time buckets of size W.
// If elapsed time into the current bucket is E, weight of the previous bucket is (W - E) / W.
// The estimated sliding window count is:
//
//	estimated = floor(prevCount * weight) + currCount
//
// Admission condition:
//
//	estimated + cost <= maxLimit
func EstimateSlidingWindow(prevCount, currCount, cost, maxLimit, windowSec, elapsedSec int64) (bool, time.Duration) {
	if windowSec <= 0 {
		windowSec = 1
	}
	if maxLimit <= 0 {
		return false, time.Duration(windowSec) * time.Second
	}
	if elapsedSec < 0 {
		elapsedSec = 0
	}
	if elapsedSec >= windowSec {
		elapsedSec = windowSec - 1
	}

	weight := float64(windowSec-elapsedSec) / float64(windowSec)
	estimated := int64(math.Floor(float64(prevCount)*weight)) + currCount

	if estimated+cost > maxLimit {
		remSec := windowSec - elapsedSec
		if remSec <= 0 {
			remSec = 1
		}
		return false, time.Duration(remSec) * time.Second
	}

	return true, 0
}

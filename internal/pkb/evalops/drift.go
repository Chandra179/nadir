package evalops

import (
	"sync"
)

// DriftDetector tracks rolling mean of a metric over a sliding window.
// Emits an alert when mean drops below baseline by more than threshold.
type DriftDetector struct {
	mu        sync.Mutex
	window    []float64
	size      int     // max window size
	baseline  float64 // expected mean (set from first full window)
	threshold float64 // relative drop that triggers alert, e.g. 0.10 = 10%
	baseSet   bool
}

func NewDriftDetector(windowSize int, dropThreshold float64) *DriftDetector {
	return &DriftDetector{
		size:      windowSize,
		threshold: dropThreshold,
	}
}

// Add appends a new observation. Returns (isDrift, currentMean).
func (d *DriftDetector) Add(v float64) (bool, float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.window = append(d.window, v)
	if len(d.window) > d.size {
		d.window = d.window[len(d.window)-d.size:]
	}

	mean := mean(d.window)

	if !d.baseSet && len(d.window) >= d.size {
		d.baseline = mean
		d.baseSet = true
		return false, mean
	}

	if d.baseSet && d.baseline > 0 {
		drop := (d.baseline - mean) / d.baseline
		return drop >= d.threshold, mean
	}
	return false, mean
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var s float64
	for _, v := range vals {
		s += v
	}
	return s / float64(len(vals))
}

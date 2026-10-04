package preprocess

import "math"

// echoCanceller removes speaker playback that leaks back into the microphone.
//
// It runs a time-domain normalised least-mean-squares (NLMS) adaptive filter
// over the speaker reference. NLMS is chosen because the reference and the
// microphone share the gateway's 20 ms frame cadence, so a circulating history
// buffer converges quickly while allocating nothing per frame.
//
// Double-talk is detected per sample with a Geigel-style comparison: when the
// microphone sample exceeds the recent reference peak by a margin, near-end
// speech is present and the filter freezes for that sample. Freezing per sample
// rather than per frame keeps the child's voice from being learned as echo even
// when they speak over the device.
type echoCanceller struct {
	weights   []float64
	history   []float64
	historyAt int
	stepSize  float64
	leakage   float64
	peak      float64
	residual  float64
	adapted   int
}

// geigelMargin is the factor by which the near-end sample must exceed the
// recent reference peak before adaptation is frozen. A value above one gives
// headroom for imperfect echo-path estimation without muting genuine speech.
const geigelMargin = 1.5

// referencePeakDecay is the per-sample decay of the tracked reference peak. At
// 16 kHz it gives roughly a 40 ms memory, matching the short echo path of a
// tabletop speaker and microphone.
const referencePeakDecay = 0.998

func newEchoCanceller(filterLength int, stepSize float64) *echoCanceller {
	return &echoCanceller{
		weights:   make([]float64, filterLength),
		history:   make([]float64, filterLength),
		historyAt: filterLength - 1,
		stepSize:  stepSize,
		leakage:   0.9999,
		residual:  1,
	}
}

// process cancels the estimated echo from one frame in place and reports whether
// near-end speech overlapped the reference.
//
// The reference slice is the speaker signal captured for the same wall-clock
// window. When it is empty or silent the microphone passes through unchanged
// and the filter keeps its state, because silence carries no echo to cancel.
func (c *echoCanceller) process(microphone []int16, reference []int16) (doubleTalk bool) {
	if c == nil || len(microphone) == 0 {
		return false
	}
	if len(reference) < len(microphone) {
		return false
	}
	filterLength := len(c.weights)

	referencePower := 0.0
	for index := range microphone {
		referencePower += float64(reference[index]) * float64(reference[index])
	}
	if referencePower < 1e-6*float64(len(microphone)) {
		return false
	}

	errorPower := 0.0
	for index := range microphone {
		referenceSample := float64(reference[index])
		c.historyAt = (c.historyAt + 1) % filterLength
		c.history[c.historyAt] = referenceSample

		if absolute := math.Abs(referenceSample); absolute > c.peak {
			c.peak = absolute
		} else {
			c.peak *= referencePeakDecay
		}

		// One pass over the history both estimates the echo and accumulates the
		// tap-vector energy the NLMS step needs.
		estimate := 0.0
		energy := 1e-9
		tap := c.historyAt
		for coefficient := 0; coefficient < filterLength; coefficient++ {
			historySample := c.history[tap]
			estimate += c.weights[coefficient] * historySample
			energy += historySample * historySample
			tap--
			if tap < 0 {
				tap = filterLength - 1
			}
		}

		microphoneSample := float64(microphone[index])
		errorSample := microphoneSample - estimate
		microphone[index] = clampInt16(errorSample)
		errorPower += errorSample * errorSample

		if c.peak > 1 && math.Abs(microphoneSample) > geigelMargin*c.peak {
			doubleTalk = true
			continue
		}

		delta := c.stepSize * errorSample / energy
		tap = c.historyAt
		for coefficient := 0; coefficient < filterLength; coefficient++ {
			c.weights[coefficient] =
				(c.weights[coefficient] + delta*c.history[tap]) * c.leakage
			tap--
			if tap < 0 {
				tap = filterLength - 1
			}
		}
		c.adapted++
	}

	meanErrorPower := errorPower / float64(len(microphone))
	c.residual = 0.9*c.residual + 0.1*meanErrorPower
	return doubleTalk
}

// reset clears the adaptive state between conversations so a new room is not
// cancelled with the previous room's echo path.
func (c *echoCanceller) reset() {
	if c == nil {
		return
	}
	for index := range c.weights {
		c.weights[index] = 0
	}
	for index := range c.history {
		c.history[index] = 0
	}
	c.historyAt = len(c.weights) - 1
	c.peak = 0
	c.residual = 1
	c.adapted = 0
}

// convergence returns a bounded 0..1024 estimate of how well the echo path is
// modelled, which the telemetry layer reports instead of raw filter energy.
func (c *echoCanceller) convergence() int {
	if c == nil {
		return 0
	}
	energy := 0.0
	for _, weight := range c.weights {
		energy += weight * weight
	}
	index := int(math.Sqrt(energy) * 1024)
	if index > 1024 {
		return 1024
	}
	return index
}

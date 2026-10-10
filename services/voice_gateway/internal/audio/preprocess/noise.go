package preprocess

import "math"

// noiseSuppressor attenuates steady background noise with per-band gains.
//
// Each 20 ms frame is split into three perfect-reconstruction bands, a noise
// power estimate is tracked for every band, and a Wiener-style gain derived from
// the band SNR is applied before the bands are summed again. Band gains rather
// than a single broadband gate keep the low rumble, the vowel region, and the
// consonant region independent, which preserves intelligibility while a fan or
// air conditioner is pushed down.
type noiseSuppressor struct {
	lowState  float64
	highState float64

	noisePower [bandCount]float64
	gain       [bandCount]float64
	primed     bool

	options     Options
	frameLength int

	// bandSamples holds the split bands for one frame so the analysis splitter
	// runs exactly once per sample and its one-pole state is not advanced twice.
	bandSamples [bandCount][]float64
}

func newNoiseSuppressor(options Options, frameLength int) *noiseSuppressor {
	suppressor := &noiseSuppressor{
		options:     options,
		frameLength: frameLength,
	}
	for band := 0; band < bandCount; band++ {
		suppressor.bandSamples[band] = make([]float64, frameLength)
		suppressor.gain[band] = 1
	}
	return suppressor
}

// process suppresses steady noise in one frame in place and reports whether any
// band was attenuated below unity gain.
func (n *noiseSuppressor) process(audioFrame []int16) bool {
	if n == nil || len(audioFrame) == 0 {
		return false
	}

	lowAlpha := onePoleAlpha(lowSplitHz, 16000)
	highAlpha := onePoleAlpha(highSplitHz, 16000)

	energy := [bandCount]float64{}
	for index := 0; index < n.frameLength; index++ {
		sample := float64(audioFrame[index])

		// First split isolates the low band; the high band is the remainder so
		// the two parts reconstruct the input exactly.
		n.lowState += lowAlpha * (sample - n.lowState)
		low := n.lowState
		midHigh := sample - low

		// Second split separates the vowel region from the consonant region.
		n.highState += highAlpha * (midHigh - n.highState)
		mid := n.highState
		high := midHigh - mid

		n.bandSamples[0][index] = low
		n.bandSamples[1][index] = mid
		n.bandSamples[2][index] = high

		energy[0] += low * low
		energy[1] += mid * mid
		energy[2] += high * high
	}

	if !n.primed {
		// The first frame seeds the estimate. Treating it as noise avoids an
		// initial burst of over-suppression before any signal is observed.
		for band := 0; band < bandCount; band++ {
			n.noisePower[band] = energy[band] / float64(n.frameLength)
			n.gain[band] = 1
		}
		n.primed = true
		return false
	}

	suppressed := false
	for band := 0; band < bandCount; band++ {
		observed := energy[band] / float64(n.frameLength)
		noise := n.noisePower[band]

		// A sudden loud burst is far more likely to be speech than a new noise
		// source, so the floor drops quickly but rises slowly. Letting it climb
		// on speech would erase the voice along with the noise.
		if observed < noise {
			noise = n.options.NoiseFloorDecayPerFrame*noise +
				(1-n.options.NoiseFloorDecayPerFrame)*observed
		} else {
			noise = n.options.NoiseFloorRisePerFrame*noise +
				(1-n.options.NoiseFloorRisePerFrame)*observed
		}
		n.noisePower[band] = noise

		target := 1.0
		if noise > 1e-9 {
			snr := math.Max(observed-noise, 0) / noise
			target = snr / (1 + snr)
			if target < n.options.NoiseSuppressionFloor {
				target = n.options.NoiseSuppressionFloor
			}
		}
		// Smoothing across frames removes the musical-noise artefacts that a
		// per-frame gain change introduces on voiced speech.
		n.gain[band] = n.options.GainSmoothingAlpha*n.gain[band] +
			(1-n.options.GainSmoothingAlpha)*target
		if n.gain[band] < 1 {
			suppressed = true
		}
	}

	for index := 0; index < n.frameLength; index++ {
		cleaned := n.gain[0]*n.bandSamples[0][index] +
			n.gain[1]*n.bandSamples[1][index] +
			n.gain[2]*n.bandSamples[2][index]
		audioFrame[index] = clampInt16(cleaned)
	}
	return suppressed
}

// noiseFloor returns the mean normalized noise estimate across the analysis
// bands. It is intentionally a scalar so telemetry never exposes spectral or
// audio content.
func (n *noiseSuppressor) noiseFloor() float64 {
	if n == nil {
		return 0
	}
	total := 0.0
	for band := 0; band < bandCount; band++ {
		total += n.noisePower[band]
	}
	return total / float64(bandCount) / (32768.0 * 32768.0)
}

// reset clears the noise estimate between conversations so a quiet room is not
// measured against the previous room's noise floor.
func (n *noiseSuppressor) reset() {
	if n == nil {
		return
	}
	n.lowState = 0
	n.highState = 0
	n.primed = false
	for band := 0; band < bandCount; band++ {
		n.noisePower[band] = 0
		n.gain[band] = 1
	}
}

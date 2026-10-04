package preprocess

import "math"

// bandCount is the number of analysis bands in the suppressor filterbank.
//
// The bands come from a tree of recursive one-pole splitters. Each splitter
// defines its high band as the exact remainder of its input, so the bands sum
// back to the original signal when every band gain is one. That property lets
// the suppressor pass clean audio through unchanged and still apply
// frequency-selective gains without an FFT or an overlap-add buffer.
const bandCount = 3

// frameSamples is the 20 ms frame length at the contract 16 kHz sample rate.
// The filterbank and gain stage are validated against it so a frame from a
// different profile is rejected rather than silently mis-processed.
const frameSamples = 320

// Split cutoff frequencies. Speech formants start near 300 Hz, so the lowest
// band isolates DC and rumble while a second split near 2 kHz separates the
// vowel-heavy region from the consonant-heavy high band where hiss dominates.
const (
	lowSplitHz  = 300.0
	highSplitHz = 2000.0
)

// onePoleAlpha converts a cutoff frequency into the smoothing coefficient of a
// one-pole low-pass filter at the contract sample rate.
func onePoleAlpha(cutoffHz float64, sampleRateHz int) float64 {
	return 1 - math.Exp(-2*math.Pi*cutoffHz/float64(sampleRateHz))
}

// clampInt16 saturates a float sample into the signed 16-bit range so a coding
// or filtering mistake can never wrap into loud noise.
func clampInt16(value float64) int16 {
	if value > math.MaxInt16 {
		return math.MaxInt16
	}
	if value < math.MinInt16 {
		return math.MinInt16
	}
	return int16(value)
}

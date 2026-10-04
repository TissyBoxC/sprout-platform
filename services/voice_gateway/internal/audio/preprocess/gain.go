package preprocess

import "math"

// gainCalibrator normalises the cleaned speech toward a target level.
//
// Recognition quality falls when a quiet, distant child and a loud, close child
// produce frames tens of decibels apart. The calibrator measures the frame RMS,
// converts the desired correction to a gain, and smooths it with separate
// attack and release rates so the gain never pumps between syllables. It runs
// last, after echo cancellation and noise suppression, so its estimate reflects
// the speech the recognizer actually receives.
type gainCalibrator struct {
	gain    float64
	options Options
	target  float64
}

func newGainCalibrator(options Options) *gainCalibrator {
	return &gainCalibrator{
		gain:    1,
		options: options,
		target:  math.Pow(10, options.TargetRmsDbfs/20) * math.MaxInt16,
	}
}

// process scales one frame toward the target level and returns the applied
// gain. The gain is clamped to the configured bounds and is additionally
// limited so the frame never clips.
func (g *gainCalibrator) process(audioFrame []int16) float64 {
	if g == nil || len(audioFrame) == 0 {
		return 1
	}

	sumSquares := 0.0
	peak := 0.0
	for _, sample := range audioFrame {
		value := float64(sample)
		sumSquares += value * value
		if absolute := math.Abs(value); absolute > peak {
			peak = absolute
		}
	}
	rms := math.Sqrt(sumSquares / float64(len(audioFrame)))

	desired := g.target / math.Max(rms, 1)
	if peak > 0 {
		// Backing off to avoid clipping keeps a sudden loud syllable from
		// wrapping and still applies the largest safe gain to that frame.
		desired = math.Min(desired, math.MaxInt16/peak)
	}
	desired = clampFloat(desired, g.options.MinGain, g.options.MaxGain)

	// Attack corrects an over-loud frame quickly; release restores gain slowly
	// so a quiet pause does not lift the noise floor into the next word.
	rate := g.options.AgcRelease
	if desired < g.gain {
		rate = g.options.AgcAttack
	}
	g.gain = rate*g.gain + (1-rate)*desired
	g.gain = clampFloat(g.gain, g.options.MinGain, g.options.MaxGain)

	for index := range audioFrame {
		audioFrame[index] = clampInt16(float64(audioFrame[index]) * g.gain)
	}
	return g.gain
}

// reset restores unity gain between conversations so the first frame is not
// scaled by the previous speaker's level.
func (g *gainCalibrator) reset() {
	if g == nil {
		return
	}
	g.gain = 1
}

func clampFloat(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

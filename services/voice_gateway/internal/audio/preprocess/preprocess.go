// Package preprocess cleans inbound device audio before it reaches the codec
// and the speech recognizer.
//
// The gateway receives microphone audio that already passed through the device
// pipeline, but speaker echo, steady background noise, and large level swings
// remain. The processor applies echo cancellation, noise suppression, and gain
// calibration in a fixed order so recognition sees a consistent 16 kHz mono
// signal regardless of room and hardware.
package preprocess

import (
	"errors"
	"fmt"
)

// Errors returned while configuring or running the processor. They are stable
// so the session layer can map them to device-facing audio error codes.
var (
	ErrFrameLength    = errors.New("audio frame length does not match the profile")
	ErrInvalidOptions = errors.New("audio preprocessing options are invalid")
)

// Options configures one preprocessing chain.
//
// Each stage is independently switchable so a profile with limited CPU can
// disable echo cancellation or noise suppression without breaking the others.
type Options struct {
	// EchoCancellation removes speaker playback leaking into the microphone.
	EchoCancellation bool
	// NoiseSuppression attenuates steady background noise.
	NoiseSuppression bool
	// GainCalibration normalizes speech toward TargetRmsDbfs.
	GainCalibration bool
	// TargetRmsDbfs is the desired speech level; typical values are -26 to -18.
	TargetRmsDbfs float64
	// EchoFilterLengthSamples is the adaptive filter length. 160 samples cover
	// the 10 ms acoustic path of the reference hardware.
	EchoFilterLengthSamples int
	// EchoStepSize is the NLMS adaptation rate in (0, 1].
	EchoStepSize float64
	// NoiseFloorDecayPerFrame slows the noise estimate on speech frames.
	NoiseFloorDecayPerFrame float64
	// NoiseFloorRisePerFrame lets the estimate follow a rising noise floor.
	NoiseFloorRisePerFrame float64
	// NoiseSuppressionFloor is the minimum retained gain per bin.
	NoiseSuppressionFloor float64
	// GainsmoothingAlpha blends per-bin gains across frames.
	GainSmoothingAlpha float64
	// MinGain and MaxGain bound the calibration gain in linear scale.
	MinGain float64
	MaxGain float64
	// AgcAttack and AgcRelease smooth gain changes (0, 1]; attack reacts to a
	// sudden drop in level, release relaxes back after speech ends.
	AgcAttack  float64
	AgcRelease float64
}

// Stats reports bounded counters for telemetry and diagnostics.
type Stats struct {
	FramesProcessed  int
	EchoCancelled    int
	NoiseSuppressed  int
	GainAdjusted     int
	DoubleTalkFrames int
	CurrentGain      float64
	// EchoConvergence is a bounded 0..1024 estimate of how well the current
	// echo path is modelled. It is reported as a scalar so callers never need
	// raw filter coefficients or audio samples.
	EchoConvergence int
	// NoiseFloor is the current normalized ambient-energy estimate.
	NoiseFloor float64
}

// Processor applies one preprocessing chain to inbound audio frames.
//
// A processor is stateful and single-stream: the adaptive echo filter and the
// noise estimate must not be shared between devices. Callers own one instance
// per session and call Reset when a conversation restarts.
type Processor interface {
	// Process cleans one frame in place and returns it.
	//
	// reference carries the speaker samples being played while the microphone
	// captured this frame; it may be nil when the speaker is silent. The frame
	// and the returned slice are the same backing array.
	Process(audioFrame []int16, reference []int16) ([]int16, error)

	// Reset clears adaptive state between conversations.
	Reset()

	// Stats returns a point-in-time snapshot.
	Stats() Stats
}

// pipeline is the default Processor implementation.
type pipeline struct {
	options     Options
	echo        *echoCanceller
	noise       *noiseSuppressor
	gain        *gainCalibrator
	frameLength int
	stats       Stats
}

// NewProcessor builds a preprocessing chain for the fixed 16 kHz mono frame
// length. Every option is validated so a misconfigured deployment fails at
// startup rather than emitting corrupted audio.
func NewProcessor(options Options, frameLength int) (Processor, error) {
	if frameLength != frameSamples {
		return nil, fmt.Errorf("%w: %d samples", ErrFrameLength, frameLength)
	}
	if err := options.validate(); err != nil {
		return nil, err
	}
	instance := &pipeline{
		options:     options,
		frameLength: frameLength,
	}
	if options.EchoCancellation {
		instance.echo = newEchoCanceller(
			options.EchoFilterLengthSamples,
			options.EchoStepSize,
		)
	}
	if options.NoiseSuppression {
		instance.noise = newNoiseSuppressor(options, frameLength)
	}
	if options.GainCalibration {
		instance.gain = newGainCalibrator(options)
	}
	return instance, nil
}

// Process runs echo cancellation, noise suppression, and gain calibration in
// that order. Echo cancellation runs first because the adaptive filter needs
// the unconcealed microphone signal, and gain calibration runs last so its
// level estimate reflects the cleaned speech.
func (p *pipeline) Process(audioFrame []int16, reference []int16) ([]int16, error) {
	if p == nil {
		return nil, errors.New("preprocessing pipeline is nil")
	}
	if len(audioFrame) != p.frameLength {
		return nil, fmt.Errorf("%w: %d samples", ErrFrameLength, len(audioFrame))
	}

	p.stats.FramesProcessed++

	if p.echo != nil {
		if doubleTalk := p.echo.process(audioFrame, reference); doubleTalk {
			p.stats.DoubleTalkFrames++
		}
		p.stats.EchoCancelled++
		p.stats.EchoConvergence = p.echo.convergence()
	}

	if p.noise != nil {
		if suppressed := p.noise.process(audioFrame); suppressed {
			p.stats.NoiseSuppressed++
		}
		p.stats.NoiseFloor = p.noise.noiseFloor()
	}

	if p.gain != nil {
		gain := p.gain.process(audioFrame)
		p.stats.CurrentGain = gain
		p.stats.GainAdjusted++
	}

	return audioFrame, nil
}

// Reset clears every adaptive stage so a new conversation starts from a clean
// estimate instead of inheriting the previous room and speaker.
func (p *pipeline) Reset() {
	if p == nil {
		return
	}
	if p.echo != nil {
		p.echo.reset()
	}
	if p.noise != nil {
		p.noise.reset()
	}
	if p.gain != nil {
		p.gain.reset()
	}
	p.stats = Stats{}
}

// Stats returns the current bounded counter snapshot.
func (p *pipeline) Stats() Stats {
	if p == nil {
		return Stats{}
	}
	return p.stats
}

func (o Options) validate() error {
	if o.EchoCancellation {
		// The canceller adapts over one whole frame, so the filter must hold at
		// least that frame; a shorter filter would see overwritten history.
		if o.EchoFilterLengthSamples < frameSamples || o.EchoFilterLengthSamples > 2048 {
			return fmt.Errorf("%w: echo filter length", ErrInvalidOptions)
		}
		if o.EchoStepSize <= 0 || o.EchoStepSize > 1 {
			return fmt.Errorf("%w: echo step size", ErrInvalidOptions)
		}
	}
	if o.NoiseSuppression {
		if o.NoiseFloorDecayPerFrame <= 0 || o.NoiseFloorDecayPerFrame > 1 ||
			o.NoiseFloorRisePerFrame <= 0 || o.NoiseFloorRisePerFrame > 1 {
			return fmt.Errorf("%w: noise floor rates", ErrInvalidOptions)
		}
		if o.NoiseSuppressionFloor < 0 || o.NoiseSuppressionFloor > 1 {
			return fmt.Errorf("%w: noise suppression floor", ErrInvalidOptions)
		}
		if o.GainSmoothingAlpha <= 0 || o.GainSmoothingAlpha > 1 {
			return fmt.Errorf("%w: gain smoothing", ErrInvalidOptions)
		}
	}
	if o.GainCalibration {
		if o.MinGain <= 0 || o.MaxGain <= 0 || o.MinGain > o.MaxGain {
			return fmt.Errorf("%w: gain bounds", ErrInvalidOptions)
		}
		if o.AgcAttack <= 0 || o.AgcAttack > 1 ||
			o.AgcRelease <= 0 || o.AgcRelease > 1 {
			return fmt.Errorf("%w: agc rates", ErrInvalidOptions)
		}
		if o.TargetRmsDbfs > 0 || o.TargetRmsDbfs < -80 {
			return fmt.Errorf("%w: target level", ErrInvalidOptions)
		}
	}
	return nil
}

// DefaultOptions returns a conservative chain suited to the reference
// hardware: echo cancellation, noise suppression, and gentle gain calibration.
func DefaultOptions() Options {
	return Options{
		EchoCancellation:        true,
		NoiseSuppression:        true,
		GainCalibration:         true,
		TargetRmsDbfs:           -24,
		EchoFilterLengthSamples: 320,
		EchoStepSize:            0.3,
		NoiseFloorDecayPerFrame: 0.999,
		NoiseFloorRisePerFrame:  0.05,
		NoiseSuppressionFloor:   0.08,
		GainSmoothingAlpha:      0.6,
		MinGain:                 0.25,
		MaxGain:                 6,
		AgcAttack:               0.6,
		AgcRelease:              0.02,
	}
}

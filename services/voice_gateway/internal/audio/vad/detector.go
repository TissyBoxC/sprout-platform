// Package vad detects speech in fixed-profile realtime audio.
//
// The detector is deliberately local and allocation-free: it runs in the
// gateway read path before any audio reaches a model provider. It consumes
// little-endian signed 16-bit PCM and keeps only statistical state, never the
// audio itself.
package vad

import (
	"encoding/binary"
	"math"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
)

const (
	defaultInitialNoiseFloor   = 0.003
	defaultNoiseFloorAdaptUp   = 0.02
	defaultNoiseFloorAdaptDown = 0.10
	defaultSpeechThreshold     = 2.8
	defaultZeroCrossingRateMax = 0.65
	defaultActivationFrames    = 3
	defaultHangoverFrames      = 18
	defaultMinimumSpeechMS     = 60
	defaultMinimumEnergy       = 0.0015
	defaultNoiseFloorMinimum   = 0.00005
	defaultNoiseFloorMaximum   = 0.20
)

// Detector reports whether one audio frame contains speech.
//
// Implementations are stateful and belong to exactly one stream. Callers must
// invoke IsSpeech in capture order. The method must not retain or mutate data.
type Detector interface {
	IsSpeech(data []byte) bool
}

// Stats reports bounded speech-detection quality for one stream.
//
// Confidence is normalized to 0..1024 and NoiseFloor is normalized ambient
// energy. The values are scalar quality signals, never audio.
type Stats struct {
	Confidence int
	NoiseFloor float64
}

// ObservingDetector is implemented by detectors that can expose quality
// metrics without changing the minimal Detector contract used by test doubles.
type ObservingDetector interface {
	Detector
	Stats() Stats
}

// Config controls the adaptive energy and zero-crossing detector.
//
// Zero values select safe defaults. Durations are expressed as frame counts
// because the realtime profile fixes one frame at 20 ms.
type Config struct {
	InitialNoiseFloor    float64
	NoiseFloorAdaptUp    float64
	NoiseFloorAdaptDown  float64
	SpeechThresholdRatio float64
	ZeroCrossingRateMax  float64
	ActivationFrames     int
	HangoverFrames       int
	MinimumSpeechMS      int
	MinimumEnergy        float64
	NoiseFloorMinimum    float64
	NoiseFloorMaximum    float64
}

// EnergyDetector uses adaptive energy plus a zero-crossing-rate guard.
//
// It is not safe for concurrent use; the session read loop calls it serially.
// State is bounded and Reset clears it between conversations.
type EnergyDetector struct {
	config           Config
	noiseFloor       float64
	activationFrames int
	hangoverFrames   int
	active           bool
	lastConfidence   int
}

// NewEnergyDetector creates a detector for the fixed 16 kHz mono frame profile.
//
// Invalid or omitted fields are replaced with bounded defaults so a
// configuration mistake cannot create a permanently active detector.
func NewEnergyDetector(config Config) *EnergyDetector {
	config = normalizeConfig(config)
	return &EnergyDetector{
		config:     config,
		noiseFloor: config.InitialNoiseFloor,
	}
}

// Config returns the effective immutable detector configuration.
func (d *EnergyDetector) Config() Config {
	if d == nil {
		return normalizeConfig(Config{})
	}
	return d.config
}

// IsSpeech reports whether the current frame is part of a speech segment.
//
// The method applies activation hysteresis and hangover so short pauses do not
// split one utterance. Malformed byte lengths are rejected without mutating
// detector state.
func (d *EnergyDetector) IsSpeech(data []byte) bool {
	if d == nil || len(data) == 0 || len(data)%2 != 0 {
		return false
	}

	energy, zeroCrossingRate := frameStatistics(data)
	threshold := math.Max(
		d.config.MinimumEnergy,
		d.noiseFloor*d.config.SpeechThresholdRatio,
	)
	candidate := energy >= threshold && zeroCrossingRate <= d.config.ZeroCrossingRateMax
	d.lastConfidence = normalizedConfidence(
		energy,
		threshold,
		zeroCrossingRate,
		d.config.ZeroCrossingRateMax,
	)

	if d.active {
		if candidate {
			d.hangoverFrames = d.config.HangoverFrames
		} else if d.hangoverFrames > 0 {
			d.hangoverFrames--
		} else {
			d.active = false
			d.activationFrames = 0
			d.adaptNoiseFloor(energy)
		}
		return d.active
	}

	if candidate {
		d.activationFrames++
		minimumActivationFrames := d.config.ActivationFrames
		requiredByDuration := millisecondsToFrames(d.config.MinimumSpeechMS)
		if requiredByDuration > minimumActivationFrames {
			minimumActivationFrames = requiredByDuration
		}
		if d.activationFrames >= minimumActivationFrames {
			d.active = true
			d.hangoverFrames = d.config.HangoverFrames
			return true
		}
		return false
	}

	d.activationFrames = 0
	d.adaptNoiseFloor(energy)
	return false
}

// Stats returns the latest bounded detector quality snapshot.
func (d *EnergyDetector) Stats() Stats {
	if d == nil {
		return Stats{}
	}
	return Stats{
		Confidence: d.lastConfidence,
		NoiseFloor: d.noiseFloor,
	}
}

// Reset clears segment state while preserving the learned noise floor.
func (d *EnergyDetector) Reset() {
	if d == nil {
		return
	}
	d.activationFrames = 0
	d.hangoverFrames = 0
	d.active = false
	d.lastConfidence = 0
}

// NoiseFloor returns the current normalized ambient-energy estimate.
func (d *EnergyDetector) NoiseFloor() float64 {
	if d == nil {
		return 0
	}
	return d.noiseFloor
}

func (d *EnergyDetector) adaptNoiseFloor(energy float64) {
	rate := d.config.NoiseFloorAdaptUp
	if energy < d.noiseFloor {
		rate = d.config.NoiseFloorAdaptDown
	}
	d.noiseFloor += rate * (energy - d.noiseFloor)
	switch {
	case d.noiseFloor < d.config.NoiseFloorMinimum:
		d.noiseFloor = d.config.NoiseFloorMinimum
	case d.noiseFloor > d.config.NoiseFloorMaximum:
		d.noiseFloor = d.config.NoiseFloorMaximum
	}
}

func normalizedConfidence(
	energy float64,
	threshold float64,
	zeroCrossingRate float64,
	maxZeroCrossingRate float64,
) int {
	if threshold <= 0 || maxZeroCrossingRate <= 0 {
		return 0
	}
	energyRatio := energy / threshold
	if energyRatio < 0 {
		energyRatio = 0
	}
	if energyRatio > 4 {
		energyRatio = 4
	}
	zcrPenalty := 1.0
	if zeroCrossingRate > maxZeroCrossingRate {
		zcrPenalty = 0
	} else {
		zcrPenalty = 1 - zeroCrossingRate/maxZeroCrossingRate
	}
	confidence := int((energyRatio/4.0)*zcrPenalty*1024 + 0.5)
	if confidence < 0 {
		return 0
	}
	if confidence > 1024 {
		return 1024
	}
	return confidence
}

func frameStatistics(data []byte) (float64, float64) {
	count := len(data) / 2
	if count == 0 {
		return 0, 0
	}

	var sumSquares float64
	var crossings int
	var previous int16
	for index := 0; index < count; index++ {
		sample := int16(binary.LittleEndian.Uint16(data[index*2:]))
		normalized := float64(sample) / 32768.0
		sumSquares += normalized * normalized
		if index > 0 && ((sample < 0 && previous >= 0) || (sample >= 0 && previous < 0)) {
			crossings++
		}
		previous = sample
	}

	energy := math.Sqrt(sumSquares / float64(count))
	zeroCrossingRate := 0.0
	if count > 1 {
		zeroCrossingRate = float64(crossings) / float64(count-1)
	}
	return energy, zeroCrossingRate
}

func millisecondsToFrames(durationMS int) int {
	if durationMS <= 0 {
		return 1
	}
	frames := (durationMS + frame.DurationMS - 1) / frame.DurationMS
	if frames < 1 {
		return 1
	}
	return frames
}

func normalizeConfig(config Config) Config {
	config.InitialNoiseFloor = bounded(
		config.InitialNoiseFloor,
		defaultInitialNoiseFloor,
		defaultNoiseFloorMinimum,
		defaultNoiseFloorMaximum,
	)
	config.NoiseFloorAdaptUp = bounded(
		config.NoiseFloorAdaptUp,
		defaultNoiseFloorAdaptUp,
		0.0001,
		1,
	)
	config.NoiseFloorAdaptDown = bounded(
		config.NoiseFloorAdaptDown,
		defaultNoiseFloorAdaptDown,
		0.0001,
		1,
	)
	config.SpeechThresholdRatio = bounded(
		config.SpeechThresholdRatio,
		defaultSpeechThreshold,
		1.01,
		100,
	)
	config.ZeroCrossingRateMax = bounded(
		config.ZeroCrossingRateMax,
		defaultZeroCrossingRateMax,
		0.001,
		1,
	)
	config.MinimumEnergy = bounded(
		config.MinimumEnergy,
		defaultMinimumEnergy,
		0,
		defaultNoiseFloorMaximum,
	)
	config.NoiseFloorMinimum = bounded(
		config.NoiseFloorMinimum,
		defaultNoiseFloorMinimum,
		0,
		defaultNoiseFloorMaximum,
	)
	config.NoiseFloorMaximum = bounded(
		config.NoiseFloorMaximum,
		defaultNoiseFloorMaximum,
		config.NoiseFloorMinimum,
		1,
	)
	config.InitialNoiseFloor = math.Max(config.InitialNoiseFloor, config.NoiseFloorMinimum)
	config.InitialNoiseFloor = math.Min(config.InitialNoiseFloor, config.NoiseFloorMaximum)

	if config.ActivationFrames <= 0 {
		config.ActivationFrames = defaultActivationFrames
	}
	if config.HangoverFrames <= 0 {
		config.HangoverFrames = defaultHangoverFrames
	}
	if config.MinimumSpeechMS <= 0 {
		config.MinimumSpeechMS = defaultMinimumSpeechMS
	}
	return config
}

func bounded(value float64, fallback float64, minimum float64, maximum float64) float64 {
	if value <= 0 {
		value = fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

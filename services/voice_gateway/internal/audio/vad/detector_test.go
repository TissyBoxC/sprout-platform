package vad

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
)

func TestEnergyDetectorRejectsSilenceAndMalformedInput(t *testing.T) {
	detector := NewEnergyDetector(Config{})
	silence := makePCM(frame.SamplesPerFrame)

	if detector.IsSpeech(nil) {
		t.Fatal("empty input must not be speech")
	}
	if detector.IsSpeech([]byte{0x01}) {
		t.Fatal("odd-length input must not be speech")
	}
	for index := 0; index < 10; index++ {
		if detector.IsSpeech(silence) {
			t.Fatalf("silence frame %d was classified as speech", index)
		}
	}
}

func TestEnergyDetectorActivatesAndHoldsThroughShortPause(t *testing.T) {
	detector := NewEnergyDetector(Config{
		MinimumSpeechMS:  40,
		ActivationFrames: 2,
		HangoverFrames:   5,
	})
	speech := makeTone(frame.SamplesPerFrame, 440, 9000)
	silence := makePCM(frame.SamplesPerFrame)

	if detector.IsSpeech(speech) {
		t.Fatal("first activation frame must respect the minimum speech duration")
	}
	if !detector.IsSpeech(speech) {
		t.Fatal("second activation frame must start speech")
	}
	for index := 0; index < 5; index++ {
		if !detector.IsSpeech(silence) {
			t.Fatalf("hangover frame %d ended speech too early", index)
		}
	}
	if detector.IsSpeech(silence) {
		t.Fatal("speech must end after the hangover window")
	}
}

func TestEnergyDetectorAdaptsNoiseFloor(t *testing.T) {
	detector := NewEnergyDetector(Config{
		InitialNoiseFloor:    0.001,
		MinimumEnergy:        0.0005,
		SpeechThresholdRatio: 3,
		NoiseFloorAdaptDown:  0.5,
		NoiseFloorAdaptUp:    0.5,
	})
	ambient := makeConstant(frame.SamplesPerFrame, 60)
	for index := 0; index < 20; index++ {
		if detector.IsSpeech(ambient) {
			t.Fatalf("ambient frame %d was classified as speech", index)
		}
	}
	if detector.NoiseFloor() <= 0.001 {
		t.Fatalf("noise floor did not adapt upward: %.6f", detector.NoiseFloor())
	}
}

func TestEnergyDetectorConfigurationIsBounded(t *testing.T) {
	detector := NewEnergyDetector(Config{
		InitialNoiseFloor:    -1,
		NoiseFloorAdaptUp:    -1,
		NoiseFloorAdaptDown:  10,
		SpeechThresholdRatio: 0,
		ZeroCrossingRateMax:  -1,
		ActivationFrames:     -1,
		HangoverFrames:       -1,
		MinimumSpeechMS:      -1,
		MinimumEnergy:        -1,
		NoiseFloorMinimum:    -1,
		NoiseFloorMaximum:    -1,
	})
	config := detector.Config()
	if config.InitialNoiseFloor <= 0 {
		t.Fatalf("initial noise floor was not defaulted: %#v", config)
	}
	if config.SpeechThresholdRatio <= 1 {
		t.Fatalf("threshold ratio was not bounded: %#v", config)
	}
	if config.NoiseFloorAdaptDown > 1 {
		t.Fatalf("adaptation rate was not bounded: %#v", config)
	}
	if config.ActivationFrames < 1 || config.HangoverFrames < 1 || config.MinimumSpeechMS < frame.DurationMS {
		t.Fatalf("frame counts were not defaulted: %#v", config)
	}
}

func TestEnergyDetectorRejectsNoiseLikeHighZeroCrossing(t *testing.T) {
	detector := NewEnergyDetector(Config{
		MinimumEnergy:        0.001,
		SpeechThresholdRatio: 1.1,
		ZeroCrossingRateMax:  0.2,
		ActivationFrames:     1,
		MinimumSpeechMS:      20,
	})
	alternating := make([]int16, frame.SamplesPerFrame)
	for index := range alternating {
		if index%2 == 0 {
			alternating[index] = 12000
		} else {
			alternating[index] = -12000
		}
	}
	if detector.IsSpeech(toPCM(alternating)) {
		t.Fatal("high zero-crossing noise must not activate speech")
	}
}

func TestEnergyDetectorStatsAreBounded(t *testing.T) {
	testCases := []struct {
		name   string
		pcm    []byte
		frames int
	}{
		{name: "speech", pcm: makeTone(frame.SamplesPerFrame, 300, 8000), frames: 3},
		{name: "silence", pcm: makePCM(frame.SamplesPerFrame), frames: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			detector := NewEnergyDetector(Config{})
			for index := 0; index < testCase.frames; index++ {
				detector.IsSpeech(testCase.pcm)
			}
			stats := detector.Stats()
			if stats.Confidence < 0 || stats.Confidence > 1024 {
				t.Fatalf("confidence outside bounds: %d", stats.Confidence)
			}
			if stats.NoiseFloor < 0 || stats.NoiseFloor > 1 {
				t.Fatalf("noise floor outside bounds: %f", stats.NoiseFloor)
			}
		})
	}
}

func makePCM(samples int) []byte {
	return make([]byte, samples*2)
}

func makeConstant(samples int, value int16) []byte {
	values := make([]int16, samples)
	for index := range values {
		values[index] = value
	}
	return toPCM(values)
}

func makeTone(samples int, frequency float64, amplitude float64) []byte {
	values := make([]int16, samples)
	for index := range values {
		sample := amplitude * math.Sin(2*math.Pi*frequency*float64(index)/frame.SampleRateHz)
		values[index] = int16(sample)
	}
	return toPCM(values)
}

func toPCM(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(data[index*2:], uint16(sample))
	}
	return data
}

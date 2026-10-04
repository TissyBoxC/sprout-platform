package preprocess

import (
	"math"
	"testing"
)

func TestProcessRejectsWrongFrameLength(t *testing.T) {
	processor, err := NewProcessor(DefaultOptions(), frameSamples)
	if err != nil {
		t.Fatalf("NewProcessor returned unexpected error: %v", err)
	}
	if _, err := processor.Process(make([]int16, frameSamples-1), nil); err == nil {
		t.Fatal("expected a frame shorter than the profile to be rejected")
	}
}

func TestProcessWithEveryStageDisabledPassesAudioThrough(t *testing.T) {
	processor, err := NewProcessor(Options{}, frameSamples)
	if err != nil {
		t.Fatalf("NewProcessor returned unexpected error: %v", err)
	}
	input := make([]int16, frameSamples)
	for index := range input {
		input[index] = int16(index * 7)
	}
	want := append([]int16(nil), input...)
	output, err := processor.Process(input, nil)
	if err != nil {
		t.Fatalf("Process returned unexpected error: %v", err)
	}
	for index := range output {
		if output[index] != want[index] {
			t.Fatalf("sample %d changed from %d to %d", index, want[index], output[index])
		}
	}
}

func TestNoiseSuppressorAttenuatesSteadyTone(t *testing.T) {
	options := DefaultOptions()
	// Disable the other stages so the assertion measures noise suppression.
	options.EchoCancellation = false
	options.GainCalibration = false
	processor, err := NewProcessor(options, frameSamples)
	if err != nil {
		t.Fatalf("NewProcessor returned unexpected error: %v", err)
	}

	tone := make([]int16, frameSamples)
	energyIn := 0.0
	energyOut := 0.0
	for frame := 0; frame < 60; frame++ {
		for index := range tone {
			phase := 2 * math.Pi * 400 * float64(frame*frameSamples+index) / 16000
			tone[index] = int16(6000 * math.Sin(phase))
		}
		for index := range tone {
			energyIn += float64(tone[index]) * float64(tone[index])
		}
		output, err := processor.Process(tone, nil)
		if err != nil {
			t.Fatalf("Process returned unexpected error: %v", err)
		}
		for index := range output {
			energyOut += float64(output[index]) * float64(output[index])
		}
	}

	if energyOut >= energyIn*0.5 {
		t.Fatalf(
			"steady tone was not suppressed: in=%0.f out=%0.f",
			energyIn,
			energyOut,
		)
	}
}

func TestGainCalibratorRaisesQuietSpeechAndLimitsClipping(t *testing.T) {
	options := DefaultOptions()
	options.EchoCancellation = false
	options.NoiseSuppression = false
	options.MinGain = 1
	options.MaxGain = 8
	calibrator := newGainCalibrator(options)

	for frame := 0; frame < 30; frame++ {
		quiet := make([]int16, frameSamples)
		for index := range quiet {
			quiet[index] = int16(40 * math.Sin(2*math.Pi*300*float64(index)/16000))
		}
		calibrator.process(quiet)
	}
	if calibrator.gain <= 1 {
		t.Fatalf("expected quiet speech to gain, got %f", calibrator.gain)
	}

	loud := make([]int16, frameSamples)
	for index := range loud {
		loud[index] = int16(32000 * math.Sin(2*math.Pi*300*float64(index)/16000))
	}
	calibrator.process(loud)
	if calibrator.gain >= 8 {
		t.Fatalf("expected loud speech to reduce gain, got %f", calibrator.gain)
	}
}

func TestEchoCancellerReducesAlignedEcho(t *testing.T) {
	canceller := newEchoCanceller(frameSamples, 0.4)
	reference := make([]int16, frameSamples)
	microphone := make([]int16, frameSamples)

	remaining := math.Inf(1)
	for frame := 0; frame < 400; frame++ {
		for index := range reference {
			sample := int16(5000 * math.Sin(2*math.Pi*500*float64(frame*frameSamples+index)/16000))
			reference[index] = sample
			// The microphone hears a delayed, attenuated copy of the speaker.
			microphone[index] = sample / 2
		}
		canceller.process(microphone, reference)
		residual := 0.0
		for _, sample := range microphone {
			residual += math.Abs(float64(sample))
		}
		remaining = residual / float64(frameSamples)
	}
	if remaining > 700 {
		t.Fatalf("echo cancellation did not converge, residual=%f", remaining)
	}
}

func TestResetClearsAdaptiveState(t *testing.T) {
	processor, err := NewProcessor(DefaultOptions(), frameSamples)
	if err != nil {
		t.Fatalf("NewProcessor returned unexpected error: %v", err)
	}
	frame := make([]int16, frameSamples)
	for index := range frame {
		frame[index] = int16(1000 * math.Sin(2*math.Pi*440*float64(index)/16000))
	}
	for frameIndex := 0; frameIndex < 10; frameIndex++ {
		if _, err := processor.Process(frame, nil); err != nil {
			t.Fatalf("Process returned unexpected error: %v", err)
		}
	}
	processor.Reset()
	if stats := processor.Stats(); stats.FramesProcessed != 0 {
		t.Fatalf("expected counters to reset, got %d frames", stats.FramesProcessed)
	}
}

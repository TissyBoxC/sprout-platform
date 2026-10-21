package domain

import (
	"errors"
	"math"
	"testing"
)

func floatPointer(value float64) *float64 {
	return &value
}

func TestValidateMemoryKeyAcceptsCanonicalKeys(t *testing.T) {
	for _, input := range []string{"favorite_color", "pet_name", " Favorite_Color "} {
		key, err := ValidateMemoryKey(input)
		if err != nil {
			t.Fatalf("ValidateMemoryKey(%q) error = %v", input, err)
		}
		if key == "" {
			t.Fatalf("ValidateMemoryKey(%q) returned an empty key", input)
		}
	}
	key, err := ValidateMemoryKey(" Favorite_Color ")
	if err != nil || key != "favorite_color" {
		t.Fatalf("key = %q, err = %v; want normalized lowercase", key, err)
	}
	// Casing is normalized rather than rejected so a device that upper-cases a
	// key still lands on the same canonical row instead of creating a duplicate.
	squashed, err := ValidateMemoryKey("FavoriteColor")
	if err != nil || squashed != "favoritecolor" {
		t.Fatalf("key = %q, err = %v; want favoritecolor", squashed, err)
	}
}

func TestValidateMemoryKeyRejectsNonCanonicalKeys(t *testing.T) {
	for _, input := range []string{"", " ", "1st_pet", "pet-name", "pet name", "a", "宠物名字"} {
		if _, err := ValidateMemoryKey(input); !errors.Is(err, ErrInvalidMemoryKey) {
			t.Fatalf("ValidateMemoryKey(%q) error = %v, want ErrInvalidMemoryKey", input, err)
		}
	}
}

func TestValidateMemoryValueTrimsAndBoundsLength(t *testing.T) {
	value, err := ValidateMemoryValue("  蓝色  ")
	if err != nil || value != "蓝色" {
		t.Fatalf("value = %q, err = %v; want trimmed value", value, err)
	}
	if _, err := ValidateMemoryValue("   "); !errors.Is(err, ErrInvalidMemoryValue) {
		t.Fatalf("blank value error = %v, want ErrInvalidMemoryValue", err)
	}
	long := make([]rune, MaxMemoryValueRunes+1)
	for index := range long {
		long[index] = '猫'
	}
	if _, err := ValidateMemoryValue(string(long)); !errors.Is(err, ErrInvalidMemoryValue) {
		t.Fatalf("overlong value error = %v, want ErrInvalidMemoryValue", err)
	}
}

func TestValidateMemoryValueRejectsControlCharacters(t *testing.T) {
	// A control character would render as a broken utterance on the device, so
	// it is rejected instead of being silently stripped.
	if _, err := ValidateMemoryValue("蓝\x00色"); !errors.Is(err, ErrInvalidMemoryValue) {
		t.Fatalf("control character error = %v, want ErrInvalidMemoryValue", err)
	}
}

func TestValidateMemoryValueErrorNeverEchoesTheValue(t *testing.T) {
	// The error must stay a sentinel: leaking the value into an error string
	// would put child personal information into logs through the wrapped error.
	_, err := ValidateMemoryValue("\x01my real home address")
	if errors.Is(err, ErrInvalidMemoryValue) && err.Error() != ErrInvalidMemoryValue.Error() {
		t.Fatalf("error text must equal the sentinel, got %q", err.Error())
	}
}

func TestNormalizeConfidenceDefaultsPerSource(t *testing.T) {
	guardian, err := NormalizeConfidence(nil, SourceGuardian)
	if err != nil || guardian != 1 {
		t.Fatalf("guardian default = %v, err = %v; want 1", guardian, err)
	}
	device, err := NormalizeConfidence(nil, SourceDevice)
	if err != nil || device != DefaultConfidenceFor(SourceDevice) {
		t.Fatalf("device default = %v, err = %v", device, err)
	}
	if _, err := NormalizeConfidence(floatPointer(-0.1), SourceDevice); !errors.Is(
		err,
		ErrInvalidMemoryConfidence,
	) {
		t.Fatalf("negative confidence error = %v", err)
	}
	if _, err := NormalizeConfidence(floatPointer(1.1), SourceDevice); !errors.Is(
		err,
		ErrInvalidMemoryConfidence,
	) {
		t.Fatalf("overconfidence error = %v", err)
	}
	// NaN must not pass the range check.
	nan := math.NaN()
	if _, err := NormalizeConfidence(&nan, SourceDevice); !errors.Is(
		err,
		ErrInvalidMemoryConfidence,
	) {
		t.Fatalf("NaN confidence error = %v", err)
	}
}

func TestValidateSource(t *testing.T) {
	for _, source := range Sources {
		if _, err := ValidateSource(" " + source + " "); err != nil {
			t.Fatalf("ValidateSource(%q) error = %v", source, err)
		}
	}
	if _, err := ValidateSource("friend"); !errors.Is(err, ErrInvalidMemorySource) {
		t.Fatalf("unknown source error = %v, want ErrInvalidMemorySource", err)
	}
}

func TestValidateMemoryInputUsesCallerSource(t *testing.T) {
	record, err := ValidateMemoryInput(MemoryInput{
		MemoryKey:   "Pet_Name",
		MemoryValue: " 咪咪 ",
	}, SourceGuardian)
	if err != nil {
		t.Fatalf("ValidateMemoryInput error = %v", err)
	}
	if record.MemoryKey != "pet_name" || record.MemoryValue != "咪咪" {
		t.Fatalf("normalized record = %+v", record)
	}
	if record.Source != SourceGuardian || record.Confidence != 1 {
		t.Fatalf("source/confidence = %s/%v, want guardian/1", record.Source, record.Confidence)
	}
}

package content_policy

import "testing"

func TestIsSafetyIntentDetectsCrisisLanguage(t *testing.T) {
	for _, text := range []string{
		"我想自杀",
		"我不想活了",
		"有人打我",
		"不要伤害自己",
		"I want to kill myself",
	} {
		if !IsSafetyIntent(text) {
			t.Fatalf("IsSafetyIntent(%q) = false, want true", text)
		}
	}
}

func TestIsSafetyIntentDoesNotFlagOrdinaryContent(t *testing.T) {
	for _, text := range []string{
		"",
		"给我讲一个睡前故事",
		"我有一点害怕黑暗",
		"今天天气真好",
	} {
		if IsSafetyIntent(text) {
			t.Fatalf("IsSafetyIntent(%q) = true, want false", text)
		}
	}
}

func TestIsKnownCategoryAndAgeTier(t *testing.T) {
	if !IsKnownCategory("story") {
		t.Fatal("story must be a known category")
	}
	if IsKnownCategory("not_a_category") {
		t.Fatal("unknown category must be rejected")
	}
	if !IsKnownAgeTier("age_3_4") || IsKnownAgeTier("age_1_2") {
		t.Fatal("age tier validation is incorrect")
	}
}

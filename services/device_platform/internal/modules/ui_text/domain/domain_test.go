package domain

import (
	"errors"
	"strings"
	"testing"
)

func validDraft() DraftInput {
	return DraftInput{
		PackageID:        "home_screen",
		Language:         "zh-CN",
		Platform:         PlatformParentApp,
		MinClientVersion: "1.0.0",
		Entries: []Entry{
			{
				UITextKey: "home.greeting",
				Value:     "早上好",
				MaxLength: 32,
				Variables: []string{"child_name"},
			},
		},
	}
}

func TestValidateDraftAcceptsWellFormedPackage(t *testing.T) {
	normalized, err := ValidateDraft(validDraft())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.PackageID != "home_screen" {
		t.Fatalf("unexpected package id: %q", normalized.PackageID)
	}
	if len(normalized.Entries) != 1 {
		t.Fatalf("unexpected entry count: %d", len(normalized.Entries))
	}
}

func TestValidateDraftRejectsBadInputs(t *testing.T) {
	cases := map[string]func(input *DraftInput){
		"blank package id":   func(in *DraftInput) { in.PackageID = "" },
		"bad language tag":   func(in *DraftInput) { in.Language = "zh_CN" },
		"unknown platform":   func(in *DraftInput) { in.Platform = Platform("smart_tv") },
		"no entries":         func(in *DraftInput) { in.Entries = nil },
		"duplicate key":      func(in *DraftInput) { in.Entries = append(in.Entries, in.Entries[0]) },
		"value over max":     func(in *DraftInput) { in.Entries[0].Value = strings.Repeat("好", 33) },
		"unknown variable":   func(in *DraftInput) { in.Entries[0].Variables = []string{"ChildName"} },
		"duplicate variable": func(in *DraftInput) { in.Entries[0].Variables = []string{"a", "a"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := validDraft()
			mutate(&input)
			if _, err := ValidateDraft(input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateTransitionLifecycle(t *testing.T) {
	if _, err := ValidateTransition(StatusDraft, ActionApprove); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
	next, err := ValidateTransition(StatusDraft, ActionSubmit)
	if err != nil || next != StatusInReview {
		t.Fatalf("submit draft: got %q, %v", next, err)
	}
	next, err = ValidateTransition(StatusInReview, ActionReject)
	if err != nil || next != StatusDraft {
		t.Fatalf("reject review: got %q, %v", next, err)
	}
	next, err = ValidateTransition(StatusInReview, ActionPublish)
	if err != nil || next != StatusPublished {
		t.Fatalf("publish review: got %q, %v", next, err)
	}
	next, err = ValidateTransition(StatusPublished, ActionWithdraw)
	if err != nil || next != StatusWithdrawn {
		t.Fatalf("withdraw published: got %q, %v", next, err)
	}
}

func TestValidSignatureRejectsBlankAndTooLong(t *testing.T) {
	if ValidSignature("") {
		t.Fatal("blank signature must be rejected")
	}
	if ValidSignature(strings.Repeat("A", MaxSignatureLength+1)) {
		t.Fatal("oversized signature must be rejected")
	}
	if !ValidSignature("AAAA") {
		t.Fatal("base64 signature should be accepted")
	}
}

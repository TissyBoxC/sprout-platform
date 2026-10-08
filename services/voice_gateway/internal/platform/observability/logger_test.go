package observability

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeErrorRedactsCauseFromErrorString(t *testing.T) {
	cause := errors.New("upstream sk-proj_1234567890abcdef child@example.com")
	err := NewSafeError("对话服务暂不可用", cause)
	if strings.Contains(err.Error(), "sk-proj_") || strings.Contains(err.Error(), "child@example.com") {
		t.Fatalf("SafeError exposed sensitive cause: %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("SafeError did not preserve the cause chain")
	}
}

func TestSafeErrorStringRedactsArbitraryErrors(t *testing.T) {
	err := errors.New("Bearer abc.def.ghi 13800138000 sk-live_1234567890abcdef")
	output := SafeErrorString(err)
	for _, forbidden := range []string{"abc.def.ghi", "13800138000", "sk-live_1234567890abcdef"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("SafeErrorString() retained %q: %q", forbidden, output)
		}
	}
}

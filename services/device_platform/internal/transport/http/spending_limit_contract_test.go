package http

import (
	"os"
	"strings"
	"testing"
)

// TestSpendingLimitContractDocumentsLimitAndRejections pins the guardian and
// operator contract for the family monthly cap: the cap itself must be an
// integer cent count, and the over-limit refusal must expose the stable code
// clients branch on.
func TestSpendingLimitContractDocumentsLimitAndRejections(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read API contract: %v", err)
	}
	body := string(contract)

	for _, path := range []string{
		"\n  /api/v1/families/{family_id}/spending-limit:",
		"\n  /api/v1/admin/families/{family_id}/spending-limit:",
		"\n  /api/v1/admin/families/{family_id}/spending-limit/rejections:",
	} {
		if !strings.Contains(body, path) {
			t.Fatalf("missing documented path %s", strings.TrimSpace(path))
		}
	}
	for _, schema := range []string{
		"\n    FamilySpendingLimit:",
		"\n    FamilySpendingLimitUpdateRequest:",
		"\n    FamilySpendingLimitRejection:",
	} {
		if !strings.Contains(body, schema) {
			t.Fatalf("missing documented schema %s", strings.TrimSpace(schema))
		}
	}
	if !strings.Contains(body, "monthly_limit_cents") {
		t.Fatal("the cap must be documented as an integer cent field")
	}
	if strings.Contains(body, "monthly_limit_yuan") {
		t.Fatal("the cap must never be documented as a yuan float")
	}
}

// TestBillingOrderContractDocumentsSpendingLimitRefusal proves the checkout
// operation tells clients which code an over-limit refusal carries; a client
// that cannot distinguish it would show a generic failure.
func TestBillingOrderContractDocumentsSpendingLimitRefusal(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read API contract: %v", err)
	}
	body := string(contract)
	if !strings.Contains(body, "spending_limit_exceeded") {
		t.Fatal("checkout contract must document the spending_limit_exceeded code")
	}
	if !strings.Contains(body, "本月消费已达上限，请先调整限额或次月再试") {
		t.Fatal("checkout contract must document the guardian-facing message")
	}
}

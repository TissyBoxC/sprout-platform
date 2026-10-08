package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/domain"
)

type stubService struct {
	requestedReason  string
	withdrawConfirm  string
	withdrawCalled   bool
	deletionResponse *domain.DeletionRequest
	status           *domain.Status
}

func (s *stubService) Status(
	_ context.Context,
	_ string,
) (*domain.Status, error) {
	if s.status != nil {
		return s.status, nil
	}
	return &domain.Status{}, nil
}

func (s *stubService) Export(
	_ context.Context,
	_ string,
) (*domain.DataExport, error) {
	return &domain.DataExport{SchemaVersion: "1.0.0"}, nil
}

func (s *stubService) RequestDeletion(
	_ context.Context,
	_ string,
	reason string,
) (*domain.DeletionRequest, error) {
	s.requestedReason = reason
	if s.deletionResponse != nil {
		return s.deletionResponse, nil
	}
	return &domain.DeletionRequest{Status: domain.DeletionStatusPending}, nil
}

func (s *stubService) CancelDeletion(
	_ context.Context,
	_ string,
) (*domain.DeletionRequest, error) {
	return &domain.DeletionRequest{Status: domain.DeletionStatusCancelled}, nil
}

func (s *stubService) WithdrawConsent(
	_ context.Context,
	_ string,
	consentType string,
	_ string,
) (*domain.Status, error) {
	s.withdrawCalled = true
	s.withdrawConfirm = consentType
	return &domain.Status{
		Consent: domain.ConsentStatus{Active: false},
	}, nil
}

func (s *stubService) GrantConsent(
	_ context.Context,
	_ string,
	_ string,
	_ string,
) (*domain.Status, error) {
	return &domain.Status{
		Consent: domain.ConsentStatus{Active: true},
	}, nil
}

func (s *stubService) QueryAudit(
	_ context.Context,
	filter domain.AuditFilter,
) (*domain.AuditPage, error) {
	return &domain.AuditPage{
		Items:    []domain.AuditEntry{},
		Total:    0,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		Actions:  []string{},
	}, nil
}

func TestRequestDeletionRequiresExactConfirmation(t *testing.T) {
	service := &stubService{}
	handler := New(service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/privacy/deletion",
		strings.NewReader(`{"confirmation":"delete","reason":"ok"}`),
	)
	recorder := httptest.NewRecorder()

	handler.RequestDeletion(recorder, request, "parent-1")

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", recorder.Code)
	}
	if service.requestedReason != "" {
		t.Fatal("deletion was requested without explicit confirmation")
	}
}

func TestRequestDeletionAcceptsExplicitConfirmation(t *testing.T) {
	service := &stubService{}
	handler := New(service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/privacy/deletion",
		strings.NewReader(`{"confirmation":"DELETE","reason":"不再使用"}`),
	)
	recorder := httptest.NewRecorder()

	handler.RequestDeletion(recorder, request, "parent-1")

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if service.requestedReason != "不再使用" {
		t.Fatalf("reason not forwarded: %q", service.requestedReason)
	}
}

func TestWithdrawConsentRequiresWithdrawWord(t *testing.T) {
	service := &stubService{}
	handler := New(service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/privacy/consent/withdraw",
		strings.NewReader(`{"consent_type":"child_data_processing","version":"2026-01"}`),
	)
	recorder := httptest.NewRecorder()

	handler.WithdrawConsent(recorder, request, "parent-1")

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", recorder.Code)
	}
	if service.withdrawCalled {
		t.Fatal("consent withdrawal ran without confirmation")
	}
}

func TestAdminAuditRejectsInvalidPageSize(t *testing.T) {
	handler := New(&stubService{})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/audit?page_size=9999",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.AdminAudit(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", recorder.Code)
	}
	var envelope struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "invalid_query" {
		t.Fatalf("unexpected error response: %s", recorder.Body.String())
	}
}

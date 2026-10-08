package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bindingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/domain"
	policydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/domain"
	usageservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/service"
)

type usageTestBindingRepository struct {
	binding *bindingdomain.Binding
	session *bindingdomain.DeviceSession
}

func (repository usageTestBindingRepository) GetDeviceSessionByTokenHash(
	_ context.Context,
	_ string,
) (*bindingdomain.DeviceSession, error) {
	if repository.session == nil {
		return nil, bindingdomain.ErrDeviceSessionNotFound
	}
	return repository.session, nil
}

func (repository usageTestBindingRepository) GetByDeviceID(
	_ context.Context,
	deviceID string,
) (*bindingdomain.Binding, error) {
	if repository.binding == nil || repository.binding.DeviceID != deviceID {
		return nil, bindingdomain.ErrDeviceNotFound
	}
	return repository.binding, nil
}

type usageTestPolicyService struct {
	effective *policydomain.EffectivePolicy
}

func (service usageTestPolicyService) GetEffective(
	_ context.Context,
	_ string,
) (*policydomain.EffectivePolicy, error) {
	return service.effective, nil
}

func TestDeviceUsageUploadRequiresMatchingSessionAndWritesFamily(t *testing.T) {
	const (
		deviceID = "sprout_device_001"
		familyID = "family_001"
	)
	repository := &usageMemoryRepository{
		usages: make(map[string]*domain.DailyUsage),
	}
	policyReader := usageTestPolicyService{
		effective: &policydomain.EffectivePolicy{
			DailyLimitMinutes: 60,
			AllowedCategories: []string{"story"},
		},
	}
	usageService, err := usageservice.New(usageservice.Options{
		Repository:    repository,
		PolicyService: policyReader,
		Clock:         usageHandlerClock{},
	})
	if err != nil {
		t.Fatalf("create usage service: %v", err)
	}
	handler := usageReportHandler{
		service: usageService,
		runtimeService: usageTestRuntimeService{
			deviceID: deviceID,
			familyID: familyID,
		},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/"+deviceID+"/runtime/usage",
		strings.NewReader(`{
			"schema_version":"1.0.0",
			"report_date":"2026-10-08",
			"timezone_offset_minutes":480,
			"active_seconds":1800,
			"conversation_count":2,
			"conversation_seconds":120,
			"content_play_count":1,
			"content_seconds":60,
			"categories":[{"category":"story","play_count":1,"seconds":60}],
			"blocked":{"disabled_period":0,"daily_limit":0,"category_denied":1,"time_untrusted":0}
		}`),
	)
	request.SetPathValue("device_id", deviceID)
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	handler.recordDeviceUsage(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	usage := repository.usages[deviceID+"/2026-10-08"]
	if usage == nil || usage.ParentAccountID != familyID ||
		usage.ActiveSeconds != 1800 {
		t.Fatalf("unexpected stored usage: %+v", usage)
	}
}

func TestDeviceUsageUploadRejectsSessionPathMismatch(t *testing.T) {
	usageService, err := usageservice.New(usageservice.Options{
		Repository: &usageMemoryRepository{usages: map[string]*domain.DailyUsage{}},
		PolicyService: usageTestPolicyService{
			effective: &policydomain.EffectivePolicy{DailyLimitMinutes: 60},
		},
		Clock: usageHandlerClock{},
	})
	if err != nil {
		t.Fatalf("create usage service: %v", err)
	}
	handler := usageReportHandler{
		service: usageService,
		runtimeService: usageTestRuntimeService{
			deviceID: "sprout_device_001",
			familyID: "family_001",
		},
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/devices/sprout_device_002/runtime/usage",
		strings.NewReader(`{
			"schema_version":"1.0.0",
			"report_date":"2026-10-08",
			"timezone_offset_minutes":480,
			"active_seconds":60,
			"conversation_count":0,
			"conversation_seconds":0,
			"content_play_count":0,
			"content_seconds":0,
			"categories":[],
			"blocked":{"disabled_period":0,"daily_limit":0,"category_denied":0,"time_untrusted":0}
		}`),
	)
	request.SetPathValue("device_id", "sprout_device_002")
	request.Header.Set("Authorization", "Bearer device-session-token")
	recorder := httptest.NewRecorder()
	handler.recordDeviceUsage(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", recorder.Code, recorder.Body.String())
	}
}

func TestGuardianUsageReportIsFamilyScoped(t *testing.T) {
	repository := &usageMemoryRepository{usages: map[string]*domain.DailyUsage{}}
	usageService, err := usageservice.New(usageservice.Options{
		Repository: repository,
		PolicyService: usageTestPolicyService{
			effective: &policydomain.EffectivePolicy{DailyLimitMinutes: 60},
		},
		Clock: usageHandlerClock{},
	})
	if err != nil {
		t.Fatalf("create usage service: %v", err)
	}
	for _, usage := range []*domain.DailyUsage{
		{ParentAccountID: "family_001", DeviceID: "sprout_device_001", ReportDate: "2026-10-08", ActiveSeconds: 600},
		{ParentAccountID: "family_002", DeviceID: "sprout_device_002", ReportDate: "2026-10-08", ActiveSeconds: 900},
	} {
		if err := repository.UpsertDaily(context.Background(), usage); err != nil {
			t.Fatalf("seed usage: %v", err)
		}
	}
	handler := usageReportHandler{service: usageService}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/usage-reports?days=1", nil)
	request = request.WithContext(context.WithValue(
		request.Context(),
		authenticatedAccountKey{},
		"family_001",
	))
	recorder := httptest.NewRecorder()
	handler.listGuardianUsageReports(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Reports []struct {
				ActiveMinutes int `json:"active_minutes"`
			} `json:"reports"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data.Reports) != 1 ||
		payload.Data.Reports[0].ActiveMinutes != 10 {
		t.Fatalf("unexpected family report: %s", recorder.Body.String())
	}
}

// TestAdminUsageReportShapesSelectedFamily proves the admin path resolves the
// requested family id and returns the same shaped report the guardian sees.
func TestAdminUsageReportShapesSelectedFamily(t *testing.T) {
	repository := &usageMemoryRepository{usages: map[string]*domain.DailyUsage{}}
	usageService, err := usageservice.New(usageservice.Options{
		Repository: repository,
		PolicyService: usageTestPolicyService{
			effective: &policydomain.EffectivePolicy{DailyLimitMinutes: 60},
		},
		Clock: usageHandlerClock{},
	})
	if err != nil {
		t.Fatalf("create usage service: %v", err)
	}
	for _, usage := range []*domain.DailyUsage{
		{
			ParentAccountID:   "family_001",
			DeviceID:          "sprout_device_001",
			DeviceName:        "客厅设备",
			ReportDate:        "2026-10-08",
			ActiveSeconds:     2400,
			ConversationCount: 4,
			Categories: []domain.CategoryUsage{
				{Category: "story", PlayCount: 2, Seconds: 600},
			},
		},
		{
			ParentAccountID: "family_002",
			DeviceID:        "sprout_device_002",
			ReportDate:      "2026-10-08",
			ActiveSeconds:   600,
		},
	} {
		if err := repository.UpsertDaily(context.Background(), usage); err != nil {
			t.Fatalf("seed usage: %v", err)
		}
	}
	handler := usageReportHandler{service: usageService}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/family_001/usage-reports?days=1",
		nil,
	)
	request.SetPathValue("parent_account_id", "family_001")
	recorder := httptest.NewRecorder()
	handler.listAdminFamilyUsageReports(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Reports []struct {
				ActiveMinutes     int `json:"active_minutes"`
				RemainingMinutes  int `json:"remaining_minutes"`
				ConversationCount int `json:"conversation_count"`
				Devices           []struct {
					DeviceID   string `json:"device_id"`
					DeviceName string `json:"device_name"`
				} `json:"devices"`
				Categories []struct {
					Category string `json:"category"`
					Minutes  int    `json:"minutes"`
				} `json:"categories"`
			} `json:"reports"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Data.Reports) != 1 {
		t.Fatalf("report count = %d, want 1: %s", len(payload.Data.Reports), recorder.Body.String())
	}
	report := payload.Data.Reports[0]
	if report.ActiveMinutes != 40 ||
		report.RemainingMinutes != 20 ||
		report.ConversationCount != 4 {
		t.Fatalf("unexpected admin report totals: %s", recorder.Body.String())
	}
	if len(report.Devices) != 1 ||
		report.Devices[0].DeviceID != "sprout_device_001" ||
		report.Devices[0].DeviceName != "客厅设备" {
		t.Fatalf("unexpected admin device breakdown: %s", recorder.Body.String())
	}
	if len(report.Categories) != 1 ||
		report.Categories[0].Category != "story" ||
		report.Categories[0].Minutes != 10 {
		t.Fatalf("unexpected admin category breakdown: %s", recorder.Body.String())
	}
}

type usageMemoryRepository struct {
	usages map[string]*domain.DailyUsage
}

func (repository *usageMemoryRepository) UpsertDaily(
	_ context.Context,
	usage *domain.DailyUsage,
) error {
	copyUsage := *usage
	repository.usages[usage.DeviceID+"/"+usage.ReportDate] = &copyUsage
	return nil
}

func (repository *usageMemoryRepository) ListByFamily(
	_ context.Context,
	parentAccountID string,
	fromDate time.Time,
	_ int,
) ([]domain.DailyUsage, error) {
	result := make([]domain.DailyUsage, 0)
	for _, usage := range repository.usages {
		if usage.ParentAccountID != parentAccountID ||
			usage.ReportDate < fromDate.Format("2006-01-02") {
			continue
		}
		result = append(result, *usage)
	}
	return result, nil
}

type usageTestRuntimeService struct {
	deviceID string
	familyID string
}

func (service usageTestRuntimeService) ResolveDeviceFamily(
	_ context.Context,
	_ string,
	pathDeviceID string,
) (string, error) {
	if pathDeviceID != service.deviceID {
		return "", bindingdomain.ErrInvalidDeviceProof
	}
	return service.familyID, nil
}

type usageHandlerClock struct{}

func (usageHandlerClock) Now() time.Time {
	return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
}

package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	billingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	billingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/service"
)

// spendingLimitServiceStub is a scripted cap service so handler tests exercise
// the HTTP shape without a database.
type spendingLimitServiceStub struct {
	view             billingservice.SpendingLimitView
	err              error
	rejections       []billingdomain.SpendingLimitRejection
	total            int
	period           billingservice.MonthPeriod
	lastFamilyID     string
	lastActor        string
	lastLimitCents   billingdomain.Cents
	lastRequestMonth time.Time
}

// GetSpendingLimit returns the scripted view.
func (stub *spendingLimitServiceStub) GetSpendingLimit(
	ctx context.Context,
	familyID string,
) (billingservice.SpendingLimitView, error) {
	stub.lastFamilyID = familyID
	return stub.view, stub.err
}

// SetSpendingLimit records the requested cap and returns the scripted view.
func (stub *spendingLimitServiceStub) SetSpendingLimit(
	ctx context.Context,
	familyID string,
	actorAccountID string,
	monthlyLimitCents billingdomain.Cents,
) (billingservice.SpendingLimitView, error) {
	stub.lastFamilyID = familyID
	stub.lastActor = actorAccountID
	stub.lastLimitCents = monthlyLimitCents
	return stub.view, stub.err
}

// ListSpendingLimitRejections returns the scripted rejections.
func (stub *spendingLimitServiceStub) ListSpendingLimitRejections(
	ctx context.Context,
	familyID string,
	moment time.Time,
	page billingservice.Page,
) ([]billingdomain.SpendingLimitRejection, int, billingservice.MonthPeriod, error) {
	stub.lastFamilyID = familyID
	stub.lastRequestMonth = moment
	return stub.rejections, stub.total, stub.period, stub.err
}

// cappedView builds a guardian view with a cap and this month's spend.
func cappedView(familyID string, limitCents int64, consumedCents int64) billingservice.SpendingLimitView {
	period := billingservice.MonthPeriodOf(
		time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
	)
	return billingservice.SpendingLimitView{
		Limit: billingdomain.FamilySpendingLimit{
			FamilyID:          familyID,
			MonthlyLimitCents: billingdomain.Cents(limitCents),
			UpdatedBy:         "owner-1",
			CreatedAt:         period.Start(),
			UpdatedAt:         period.Start().Add(time.Hour),
		},
		Spending: billingdomain.MonthlySpending{
			FamilyID:      familyID,
			Period:        period.Start(),
			ConsumedCents: billingdomain.Cents(consumedCents),
		},
	}
}

// spendingLimitRoutePatterns are the routes that must exist whenever billing is
// wired. A missing pattern answers 404, which a guardian cannot tell apart from
// "this deployment is too old".
var spendingLimitRoutePatterns = []string{
	"GET /api/v1/families/{family_id}/spending-limit",
	"PUT /api/v1/families/{family_id}/spending-limit",
	"GET /api/v1/admin/families/{family_id}/spending-limit",
	"GET /api/v1/admin/families/{family_id}/spending-limit/rejections",
}

// TestSpendingLimitRoutePatternsAreDeclared guards the transport half of the
// wiring by inspecting the route source, matching how the other billing route
// families are pinned.
func TestSpendingLimitRoutePatternsAreDeclared(t *testing.T) {
	source, err := os.ReadFile("billing_routes.go")
	if err != nil {
		t.Fatalf("read billing_routes.go: %v", err)
	}
	body := string(source)
	for _, pattern := range spendingLimitRoutePatterns {
		if !strings.Contains(body, `"`+pattern+`"`) {
			t.Errorf("route pattern not registered in billing_routes.go: %s", pattern)
		}
	}
}

// TestSpendingLimitRouteProtectedByFamilyAuthorization proves the guardian
// routes are mounted behind authentication and the canonical family actions
// rather than being reachable anonymously.
func TestSpendingLimitRouteProtectedByFamilyAuthorization(t *testing.T) {
	options := newTestRouterOptions()
	options.Billing = BillingOptions{Service: &billingservice.Service{}}
	options.AuthService = &authservice.Service{}
	router := NewRouter(options)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/families/family-1/spending-limit",
		nil,
	)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without a session", recorder.Code)
	}
}

// TestGetLimitReturnsCapAndSpend proves the guardian read reports the cap, the
// month's spend and the derived remaining amount.
func TestGetLimitReturnsCapAndSpend(t *testing.T) {
	stub := &spendingLimitServiceStub{view: cappedView("family-1", 19900, 5000)}
	handler := spendingLimitHandler{service: stub}
	request := englishRequest(http.MethodGet, "/api/v1/families/family-1/spending-limit", "", "family-1")
	recorder := httptest.NewRecorder()

	handler.getLimit(recorder, request, "family-1")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			MonthlyLimitCents int64  `json:"monthly_limit_cents"`
			ConsumedCents     int64  `json:"consumed_cents"`
			RemainingCents    int64  `json:"remaining_cents"`
			IsUnlimited       bool   `json:"is_unlimited"`
			Period            string `json:"period"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.MonthlyLimitCents != 19900 ||
		payload.Data.ConsumedCents != 5000 ||
		payload.Data.RemainingCents != 14900 {
		t.Fatalf("unexpected amounts: %+v", payload.Data)
	}
	if payload.Data.IsUnlimited {
		t.Fatal("a positive cap must not report unlimited")
	}
	if payload.Data.Period != "2026-10" {
		t.Fatalf("period = %q, want 2026-10", payload.Data.Period)
	}
}

// TestGetLimitHidesAnotherFamily proves a path family that is not the caller's
// own family is answered as "not found" rather than leaking its cap.
func TestGetLimitHidesAnotherFamily(t *testing.T) {
	stub := &spendingLimitServiceStub{view: cappedView("family-2", 19900, 0)}
	handler := spendingLimitHandler{service: stub}
	request := englishRequest(http.MethodGet, "/api/v1/families/family-2/spending-limit", "", "family-1")
	recorder := httptest.NewRecorder()

	handler.getLimit(recorder, request, "family-2")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another family", recorder.Code)
	}
	if stub.lastFamilyID != "" {
		t.Fatal("the service must not be called for another family")
	}
}

// TestSetLimitRecordsTheActingGuardian proves the resolved account, not a body
// field, is recorded as the modifier.
func TestSetLimitRecordsTheActingGuardian(t *testing.T) {
	stub := &spendingLimitServiceStub{view: cappedView("family-1", 30000, 0)}
	handler := spendingLimitHandler{service: stub}
	request := englishRequest(
		http.MethodPut,
		"/api/v1/families/family-1/spending-limit",
		`{"monthly_limit_cents":30000}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()

	handler.setLimit(recorder, request, "family-1")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.lastLimitCents != 30000 {
		t.Fatalf("limit = %d, want 30000", int64(stub.lastLimitCents))
	}
	if stub.lastActor != "family-1" {
		t.Fatalf("actor = %q, want the authenticated account", stub.lastActor)
	}
}

// TestSetLimitRequiresTheAmountField proves an omitted field is refused rather
// than decoded as zero and silently removing the cap.
func TestSetLimitRequiresTheAmountField(t *testing.T) {
	stub := &spendingLimitServiceStub{}
	handler := spendingLimitHandler{service: stub}
	request := englishRequest(
		http.MethodPut,
		"/api/v1/families/family-1/spending-limit",
		`{}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()

	handler.setLimit(recorder, request, "family-1")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if stub.lastFamilyID != "" {
		t.Fatal("an incomplete request must not reach the service")
	}
}

// TestSetLimitMapsInvalidAmount proves an out-of-range cap is a validation
// failure, not an internal error.
func TestSetLimitMapsInvalidAmount(t *testing.T) {
	stub := &spendingLimitServiceStub{err: billingdomain.ErrInvalidSpendingLimit}
	handler := spendingLimitHandler{service: stub}
	request := englishRequest(
		http.MethodPut,
		"/api/v1/families/family-1/spending-limit",
		`{"monthly_limit_cents":-1}`,
		"family-1",
	)
	recorder := httptest.NewRecorder()

	handler.setLimit(recorder, request, "family-1")

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
}

// TestCheckoutOverLimitRendersTheStableCode proves the checkout route answers
// the agreed code and Chinese message, not a generic conflict.
func TestCheckoutOverLimitRendersTheStableCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/billing/orders", nil)

	writeBillingError(
		recorder,
		request,
		billingdomain.ErrSpendingLimitExceeded,
	)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code != "spending_limit_exceeded" {
		t.Fatalf("code = %q, want spending_limit_exceeded", payload.Error.Code)
	}
	if payload.Error.Message != "本月消费已达上限，请先调整限额或次月再试" {
		t.Fatalf("message = %q", payload.Error.Message)
	}
}

// TestAdminListRejectionsParsesThePeriodFilter proves an invalid month is
// refused instead of silently reporting another month.
func TestAdminListRejectionsParsesThePeriodFilter(t *testing.T) {
	stub := &spendingLimitServiceStub{
		rejections: []billingdomain.SpendingLimitRejection{{
			ID:                "rejection-1",
			FamilyID:          "family-1",
			MonthlyLimitCents: 1000,
			ConsumedCents:     1000,
			RequestedCents:    9900,
			PlanCode:          "sprout_month",
			CreatedAt:         time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		}},
		total:  1,
		period: billingservice.MonthPeriodOf(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)),
	}
	handler := spendingLimitHandler{service: stub}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/family-1/spending-limit/rejections?period=2026-09",
		nil,
	)
	request.SetPathValue("family_id", "family-1")
	recorder := httptest.NewRecorder()

	handler.adminListRejections(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.lastRequestMonth.Month() != time.September {
		t.Fatalf("requested month = %s, want September", stub.lastRequestMonth)
	}

	badRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families/family-1/spending-limit/rejections?period=2026-13",
		nil,
	)
	badRequest.SetPathValue("family_id", "family-1")
	badRecorder := httptest.NewRecorder()
	handler.adminListRejections(badRecorder, badRequest)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an invalid month", badRecorder.Code)
	}
}

// TestAdminGetLimitRequiresTheFamily proves an empty target family is refused.
func TestAdminGetLimitRequiresTheFamily(t *testing.T) {
	handler := spendingLimitHandler{service: &spendingLimitServiceStub{}}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/families//spending-limit",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.adminGetLimit(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// TestSpendingLimitErrorsStayGeneric proves a storage failure never leaks its
// cause to the guardian.
func TestSpendingLimitErrorsStayGeneric(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/families/family-1/spending-limit", nil)

	writeSpendingLimitError(recorder, request, errors.New("pq: relation does not exist"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "relation") {
		t.Fatal("internal storage detail leaked to the guardian")
	}
}

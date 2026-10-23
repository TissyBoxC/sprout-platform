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

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	billingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	billingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/service"
)

// refundListService is a billing service whose only implemented method is the
// operator refund listing.
//
// The other methods are reached through the embedded nil interface: a test that
// calls one of them panics on the spot rather than silently getting a zero value,
// which is exactly the failure a wrong handler wiring should produce. listCalls
// counts the reads so a denied request can be proven never to reach the service.
type refundListService struct {
	billingService
	listings  []repository.RefundListing
	total     int
	err       error
	listCalls int
	lastList  repository.RefundFilter
}

// ListRefunds returns the scripted page and records the filter it received.
func (service *refundListService) ListRefunds(
	ctx context.Context,
	filter repository.RefundFilter,
) ([]repository.RefundListing, int, error) {
	service.listCalls++
	service.lastList = filter
	return service.listings, service.total, service.err
}

// refundListing builds one operator refund row with a resolved order number.
func refundListing(refundNo string, status billingdomain.RefundStatus) repository.RefundListing {
	requestedAt := time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC)
	return repository.RefundListing{
		Refund: billingdomain.Refund{
			ID:          "refund-" + refundNo,
			RefundNo:    refundNo,
			OrderID:     "order-1",
			AmountCents: 9900,
			Reason:      "家里暂时不需要这个套餐了",
			Status:      status,
			RequestedAt: requestedAt,
			CreatedAt:   requestedAt,
			UpdatedAt:   requestedAt,
		},
		OrderNo: "SP20261020A1B2C3",
	}
}

// adminRefundsPayload is the console-shaped body the page reads.
type adminRefundsPayload struct {
	Data struct {
		Refunds []map[string]any `json:"refunds"`
		Total   int              `json:"total"`
	} `json:"data"`
}

// decodeAdminRefunds decodes an operator refund listing response.
func decodeAdminRefunds(t *testing.T, recorder *httptest.ResponseRecorder) adminRefundsPayload {
	t.Helper()
	var payload adminRefundsPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode refund list: %v (body %s)", err, recorder.Body.String())
	}
	return payload
}

// TestAdminListRefundsReturnsAPageInConsoleFieldNames proves one page of refunds
// is answered with exactly the field names the console reads, including the
// order number it joins in and an empty string for a value that is still NULL.
func TestAdminListRefundsReturnsAPageInConsoleFieldNames(t *testing.T) {
	service := &refundListService{
		listings: []repository.RefundListing{
			refundListing("SR20261020ABCDEFGH", billingdomain.RefundStatusRequested),
		},
		total: 1,
	}
	handler := billingHandler{service: service}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/billing/refunds?status=requested&limit=10&offset=20",
		nil,
	)
	recorder := httptest.NewRecorder()

	handler.adminListRefunds(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if service.lastList.Status != billingdomain.RefundStatusRequested ||
		service.lastList.Limit != 10 ||
		service.lastList.Offset != 20 {
		t.Fatalf("filter = %+v, want the requested status and window", service.lastList)
	}
	payload := decodeAdminRefunds(t, recorder)
	if payload.Data.Total != 1 || len(payload.Data.Refunds) != 1 {
		t.Fatalf("total = %d, refunds = %d, want 1 and 1", payload.Data.Total, len(payload.Data.Refunds))
	}
	refund := payload.Data.Refunds[0]
	// The console only renders a row whose refund_no is present, so a renamed
	// field would empty the approval queue without any error.
	if refund["refund_no"] != "SR20261020ABCDEFGH" {
		t.Fatalf("refund_no = %#v", refund["refund_no"])
	}
	if refund["order_no"] != "SP20261020A1B2C3" {
		t.Fatalf("order_no = %#v, want the joined order number", refund["order_no"])
	}
	if refund["amount_cny_cents"] != float64(9900) {
		t.Fatalf("amount_cny_cents = %#v, want the integer cent amount", refund["amount_cny_cents"])
	}
	if refund["requested_at"] != "2026-10-20T11:00:00Z" {
		t.Fatalf("requested_at = %#v", refund["requested_at"])
	}
	// A nullable instant is rendered as "" so the page treats it as "no record
	// yet" instead of failing to format a null.
	for _, field := range []string{"approved_at", "completed_at"} {
		if value, ok := refund[field]; !ok || value != "" {
			t.Fatalf("%s = %#v (present=%v), want an empty string", field, value, ok)
		}
	}
	for _, field := range []string{"id", "order_id", "reason", "operator_account_id", "status", "channel_refund_no"} {
		if _, ok := refund[field]; !ok {
			t.Fatalf("missing documented field %q in %#v", field, refund)
		}
	}
}

// TestAdminListRefundsReturnsEmptyArray proves an empty queue is an empty JSON
// array, not null, so the page can render its empty state instead of a crash.
func TestAdminListRefundsReturnsEmptyArray(t *testing.T) {
	service := &refundListService{listings: []repository.RefundListing{}, total: 0}
	handler := billingHandler{service: service}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/refunds", nil)
	recorder := httptest.NewRecorder()

	handler.adminListRefunds(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, `"refunds":null`) {
		t.Fatalf("an empty listing must serialise as [], got %s", body)
	}
	payload := decodeAdminRefunds(t, recorder)
	if payload.Data.Refunds == nil {
		t.Fatal("refunds must decode to an empty slice, not nil")
	}
	if len(payload.Data.Refunds) != 0 || payload.Data.Total != 0 {
		t.Fatalf("refunds = %d, total = %d, want 0 and 0", len(payload.Data.Refunds), payload.Data.Total)
	}
}

// TestAdminListRefundsRefusesInvalidPaging proves a malformed window is a client
// error before the service is asked, matching the order listing.
func TestAdminListRefundsRefusesInvalidPaging(t *testing.T) {
	service := &refundListService{}
	handler := billingHandler{service: service}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/refunds?limit=-1", nil)
	recorder := httptest.NewRecorder()

	handler.adminListRefunds(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if service.lastList != (repository.RefundFilter{}) {
		t.Fatal("an invalid window must not reach the service")
	}
}

// TestAdminListRefundsRefusesUnknownStatus proves an undeclared status is a
// validation error rather than a silently empty queue.
func TestAdminListRefundsRefusesUnknownStatus(t *testing.T) {
	service := &refundListService{
		err: errors.Join(billingservice.ErrInvalidRequest, errors.New("unknown refund status")),
	}
	handler := billingHandler{service: service}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/refunds?status=refunded", nil)
	recorder := httptest.NewRecorder()

	handler.adminListRefunds(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
	}
}

// TestAdminListRefundsRequiresBillingRead proves the route rejects an
// administrator without billing.read and lets one with it through.
func TestAdminListRefundsRequiresBillingRead(t *testing.T) {
	service := &refundListService{listings: []repository.RefundListing{}, total: 0}
	handler := billingHandler{service: service}

	deniedHandler, deniedToken := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:               "admin-1",
		Role:             authdomain.RoleAdmin,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionBillingManage},
	})
	deniedRequest := authenticatedRequest(deniedToken)
	deniedRequest.URL.Path = "/api/v1/admin/billing/refunds"
	deniedRecorder := httptest.NewRecorder()

	deniedHandler.requireAdminPermission(
		authdomain.AdminPermissionBillingRead,
		handler.adminListRefunds,
	)(deniedRecorder, deniedRequest)

	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without billing.read", deniedRecorder.Code)
	}
	if service.listCalls != 0 {
		t.Fatalf("a denied request reached the service %d times", service.listCalls)
	}

	grantedHandler, grantedToken := newPermissionMiddleware(t, &authdomain.ParentAccount{
		ID:               "admin-2",
		Role:             authdomain.RoleAdmin,
		Status:           "active",
		AdminPermissions: []string{authdomain.AdminPermissionBillingRead},
	})
	grantedRequest := authenticatedRequest(grantedToken)
	grantedRequest.URL.Path = "/api/v1/admin/billing/refunds"
	grantedRecorder := httptest.NewRecorder()

	grantedHandler.requireAdminPermission(
		authdomain.AdminPermissionBillingRead,
		handler.adminListRefunds,
	)(grantedRecorder, grantedRequest)

	if grantedRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with billing.read: %s",
			grantedRecorder.Code, grantedRecorder.Body.String())
	}
	if service.listCalls != 1 {
		t.Fatalf("granted request reached the service %d times, want 1", service.listCalls)
	}
}

// TestAdminRefundListRouteIsDeclared proves the route exists in the source of
// registerBillingRoutes, matching how the other billing patterns are pinned.
func TestAdminRefundListRouteIsDeclared(t *testing.T) {
	source, err := os.ReadFile("billing_routes.go")
	if err != nil {
		t.Fatalf("read billing_routes.go: %v", err)
	}
	if !strings.Contains(string(source), `"GET /api/v1/admin/billing/refunds"`) {
		t.Fatal("the operator refund listing route is not registered")
	}
}

// TestAdminRefundListContractDocumentsTheConsoleFields proves the OpenAPI
// contract declares the listing with the console's field names, so a client
// generated from the contract cannot disagree with the handler.
func TestAdminRefundListContractDocumentsTheConsoleFields(t *testing.T) {
	contract, err := os.ReadFile("../../contracts/http/openapi.yaml")
	if err != nil {
		t.Fatalf("read API contract: %v", err)
	}
	body := string(contract)
	for _, fragment := range []string{
		"\n  /api/v1/admin/billing/refunds:",
		"\n    BillingAdminRefund:",
		"\n    BillingRefundsEnvelope:",
		"order_no:",
		"operator_account_id:",
		"channel_refund_no:",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("missing documented fragment %q", strings.TrimSpace(fragment))
		}
	}
}

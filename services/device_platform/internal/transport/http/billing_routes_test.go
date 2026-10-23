package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// billingRoutePatterns are the routes that must exist whenever the billing
// service is wired. A missing pattern answers 404, which a paying guardian
// cannot tell apart from "this deployment is too old".
var billingRoutePatterns = []string{
	"GET /api/v1/billing/plans",
	"GET /api/v1/billing/orders",
	"GET /api/v1/billing/orders/{order_no}",
	"POST /api/v1/billing/orders",
	"POST /api/v1/billing/orders/{order_no}/cancel",
	"POST /api/v1/billing/orders/{order_no}/pay",
	"POST /api/v1/billing/orders/{order_no}/refund",
	"GET /api/v1/billing/consent",
	"POST /api/v1/billing/consent",
	"DELETE /api/v1/billing/consent",
	"GET /api/v1/admin/billing/orders",
	"GET /api/v1/admin/billing/orders/{order_no}",
	"GET /api/v1/admin/billing/plans",
	"POST /api/v1/admin/billing/plans",
	"PUT /api/v1/admin/billing/plans/{plan_id}",
	"GET /api/v1/admin/billing/refunds",
	"POST /api/v1/admin/billing/refunds/{refund_no}/approve",
	"POST /api/v1/admin/billing/refunds/{refund_no}/reject",
	"GET /api/v1/admin/billing/reconciliation",
	"POST /api/v1/admin/billing/reconciliation",
	"GET /api/v1/admin/billing/reconciliation/{run_id}",
	"GET /api/v1/admin/billing/channels",
}

// TestBillingRoutePatternsAreDeclared guards the transport half of the billing
// wiring: every pattern must be registered inside registerBillingRoutes. The
// complementary half lives in internal/app, where a service that is built but
// never handed to RouterOptions would leave these patterns unreachable.
//
// The source is inspected rather than the live mux because constructing a
// billing service needs a full repository, and this test exists to catch a
// missing registration line, not to exercise handlers.
func TestBillingRoutePatternsAreDeclared(t *testing.T) {
	source, err := os.ReadFile("billing_routes.go")
	if err != nil {
		t.Fatalf("read billing_routes.go: %v", err)
	}
	body := string(source)

	for _, pattern := range billingRoutePatterns {
		if !strings.Contains(body, `"`+pattern+`"`) {
			t.Errorf("route pattern not registered in billing_routes.go: %s", pattern)
		}
	}
}

// TestPaymentCallbackRegisteredWithoutBillingService pins the one route that
// must survive a disabled billing stack: a provider retries for hours and must
// receive a definitive answer rather than a 404 that looks like a deployment
// error.
func TestPaymentCallbackRegisteredWithoutBillingService(t *testing.T) {
	mux := http.NewServeMux()
	registerBillingRoutes(mux, RouterOptions{}, newTestLogger())

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/billing/payments/notify/wechat",
		nil,
	)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusNotFound {
		t.Fatal("payment callback must stay registered when billing is disabled")
	}
}

// TestGuardianRoutesAbsentWhenBillingServiceIsNil documents the failure mode
// that shipped in 0.33.0. When RouterOptions.Billing.Service is nil the
// guardian and operator routes are intentionally skipped, so a caller that
// forgets to assign Billing gets a healthy process with a 404-only surface.
// This test is the reason the app-level wiring test exists.
func TestGuardianRoutesAbsentWhenBillingServiceIsNil(t *testing.T) {
	mux := http.NewServeMux()
	registerBillingRoutes(mux, RouterOptions{}, newTestLogger())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/billing/plans", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"expected 404 with a nil billing service, got %d",
			recorder.Code,
		)
	}
}

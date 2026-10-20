package http

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	billingdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/repository"
	billingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/service"
)

// billingService is the narrow billing surface the transport layer depends on.
//
// It exists so HTTP tests can drive the routes without a database, and so the
// transport never reaches into the billing repository or domain internals. The
// concrete implementation is the *Service built in the composition root; every
// money decision stays there.
type billingService interface {
	ListActivePlans(ctx context.Context) ([]billingdomain.Plan, error)
	ListPlans(ctx context.Context, status billingdomain.PlanStatus, search string, page billingservice.Page) ([]billingdomain.Plan, int, error)
	CreatePlan(ctx context.Context, input billingservice.PlanInput) (*billingdomain.Plan, error)
	UpdatePlan(ctx context.Context, planID string, expectedVersion int, input billingservice.PlanInput) (*billingdomain.Plan, error)

	CreateOrder(ctx context.Context, request billingservice.CreateOrderRequest) (billingservice.CreateOrderResult, error)
	GetOrderForParent(ctx context.Context, parentAccountID string, orderNo string) (*billingdomain.Order, error)
	ListOrdersForParent(ctx context.Context, parentAccountID string, page billingservice.Page) ([]billingdomain.Order, int, error)
	ListOrders(ctx context.Context, filter repository.OrderFilter) ([]billingdomain.Order, int, error)
	CancelOrder(ctx context.Context, parentAccountID string, orderNo string) (*billingdomain.Order, error)
	FindOrderByNumber(ctx context.Context, orderNo string) (*billingdomain.Order, error)

	StartPayment(ctx context.Context, parentAccountID string, orderNo string, channelName string) (billingservice.StartPaymentResult, error)
	HandleNotification(ctx context.Context, channelName string, request channel.NotificationRequest) (billingservice.NotificationOutcome, error)

	RequestRefund(ctx context.Context, request billingservice.RequestRefundRequest) (*billingdomain.Refund, error)
	ApproveRefund(ctx context.Context, operatorAccountID string, refundNo string) (*billingdomain.Refund, error)
	RejectRefund(ctx context.Context, operatorAccountID string, refundNo string) (*billingdomain.Refund, error)

	GetActiveConsent(ctx context.Context, parentAccountID string, consentVersion string) (*billingdomain.SpendingConsent, error)
	GrantConsent(ctx context.Context, parentAccountID string, consentVersion string, clientIP string) (*billingdomain.SpendingConsent, error)
	RevokeConsent(ctx context.Context, parentAccountID string, consentVersion string) (bool, error)

	RunReconciliation(ctx context.Context, channelName string, businessDate time.Time) (*billingdomain.ReconciliationRun, error)
	ListReconciliationRuns(ctx context.Context, page billingservice.Page) ([]billingdomain.ReconciliationRun, error)
	ListReconciliationItems(ctx context.Context, runID string, unresolvedOnly bool) ([]billingdomain.ReconciliationItem, error)
}

// billingHandler translates guardian checkout, operator console, and payment
// provider callbacks onto the billing service.
//
// Boundary: it owns HTTP shape only. Money decisions (state machines, refundable
// limits, callback settlement) stay in the service layer, so a transport change
// can never move money. Every amount crosses the wire as an integer cent field
// named *_cny_cents; no float or formatted string is emitted.
type billingHandler struct {
	service  billingService
	channels *channel.Registry
	logger   *slog.Logger
}

// newBillingHandler builds the billing HTTP handler. A nil service disables
// every route it owns.
//
// A typed-nil concrete service is normalised to a true nil interface here. The
// call site passes *billingservice.Service, so an unwired RouterOptions yields
// an interface holding a nil pointer, which compares unequal to nil and would
// defeat every `service == nil` guard below.
func newBillingHandler(
	service billingService,
	channels *channel.Registry,
	logger *slog.Logger,
) billingHandler {
	if logger == nil {
		logger = slog.Default()
	}
	if concrete, ok := service.(*billingservice.Service); ok && concrete == nil {
		service = nil
	}
	return billingHandler{service: service, channels: channels, logger: logger}
}

// --- wire shapes -----------------------------------------------------------

// planResponse is the guardian and operator view of one plan.
//
// AIQuotaUSD is a provider decimal string, not money: it is passed through as a
// string so no float ever represents it.
type planResponse struct {
	PlanID                string     `json:"plan_id"`
	Code                  string     `json:"code"`
	DisplayName           string     `json:"display_name"`
	Description           string     `json:"description"`
	BillingPeriod         string     `json:"billing_period"`
	PeriodMonths          int        `json:"period_months"`
	PriceCNYCents         int64      `json:"price_cny_cents"`
	OriginalPriceCNYCents int64      `json:"original_price_cny_cents"`
	AIQuotaUSD            string     `json:"ai_quota_usd"`
	Status                string     `json:"status"`
	SortOrder             int        `json:"sort_order"`
	Version               int        `json:"version"`
	PublishedAt           *time.Time `json:"published_at,omitempty"`
}

// orderResponse is one guardian order. Every amount is CNY cents.
type orderResponse struct {
	OrderNo            string     `json:"order_no"`
	PlanCode           string     `json:"plan_code"`
	PlanDisplayName    string     `json:"plan_display_name"`
	BillingPeriod      string     `json:"billing_period"`
	PeriodMonths       int        `json:"period_months"`
	UnitPriceCNYCents  int64      `json:"unit_price_cny_cents"`
	DiscountCNYCents   int64      `json:"discount_cny_cents"`
	PayableCNYCents    int64      `json:"payable_cny_cents"`
	PaidCNYCents       int64      `json:"paid_cny_cents"`
	RefundedCNYCents   int64      `json:"refunded_cny_cents"`
	RefundableCNYCents int64      `json:"refundable_cny_cents"`
	Status             string     `json:"status"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	PaidAt             *time.Time `json:"paid_at,omitempty"`
	CancelledAt        *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

// refundResponse is one guardian or operator refund. Amounts are CNY cents.
type refundResponse struct {
	RefundNo       string     `json:"refund_no"`
	AmountCNYCents int64      `json:"amount_cny_cents"`
	Reason         string     `json:"reason"`
	Status         string     `json:"status"`
	RequestedAt    time.Time  `json:"requested_at"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// consentResponse reports the guardian spending consent state.
type consentResponse struct {
	ConsentVersion string     `json:"consent_version"`
	Method         string     `json:"method"`
	GrantedAt      *time.Time `json:"granted_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	IsActive       bool       `json:"is_active"`
}

// reconciliationRunResponse is one reconciliation run. Amounts are CNY cents
// and the difference is signed: negative means the platform recorded more.
type reconciliationRunResponse struct {
	RunID                 string     `json:"run_id"`
	Channel               string     `json:"channel"`
	BusinessDate          string     `json:"business_date"`
	Status                string     `json:"status"`
	ChannelTotalCNYCents  int64      `json:"channel_total_cny_cents"`
	PlatformTotalCNYCents int64      `json:"platform_total_cny_cents"`
	DifferenceCNYCents    int64      `json:"difference_cny_cents"`
	MatchedCount          int        `json:"matched_count"`
	MissingCount          int        `json:"missing_count"`
	ExtraCount            int        `json:"extra_count"`
	ErrorMessage          string     `json:"error_message"`
	StartedAt             time.Time  `json:"started_at"`
	FinishedAt            *time.Time `json:"finished_at,omitempty"`
}

// reconciliationItemResponse is one matched or discrepant transaction.
type reconciliationItemResponse struct {
	ItemID           string `json:"item_id"`
	ChannelTradeNo   string `json:"channel_trade_no"`
	OrderNo          string `json:"order_no"`
	ChannelCNYCents  int64  `json:"channel_cny_cents"`
	PlatformCNYCents int64  `json:"platform_cny_cents"`
	Discrepancy      string `json:"discrepancy"`
	Resolved         bool   `json:"resolved"`
}

// --- request shapes --------------------------------------------------------

type createBillingOrderRequest struct {
	PlanCode       string `json:"plan_code"`
	IdempotencyKey string `json:"idempotency_key"`
	ConsentVersion string `json:"consent_version"`
}

type startBillingPaymentRequest struct {
	Channel string `json:"channel"`
}

// requestBillingRefundRequest must always carry the amount: the service refuses
// a non-positive refund, and defaulting one here would decide money in
// transport.
type requestBillingRefundRequest struct {
	AmountCNYCents int64  `json:"amount_cny_cents"`
	Reason         string `json:"reason"`
}

type grantBillingConsentRequest struct {
	ConsentVersion string `json:"consent_version"`
}

type billingPlanWriteRequest struct {
	PlanID                string `json:"plan_id"`
	Code                  string `json:"code"`
	DisplayName           string `json:"display_name"`
	Description           string `json:"description"`
	BillingPeriod         string `json:"billing_period"`
	PeriodMonths          int    `json:"period_months"`
	PriceCNYCents         int64  `json:"price_cny_cents"`
	OriginalPriceCNYCents int64  `json:"original_price_cny_cents"`
	AIQuotaUSD            string `json:"ai_quota_usd"`
	Status                string `json:"status"`
	SortOrder             int    `json:"sort_order"`
	Version               int    `json:"version"`
}

type startBillingReconciliationRequest struct {
	Channel      string `json:"channel"`
	BusinessDate string `json:"business_date"`
}

// --- guardian handlers -----------------------------------------------------

// listPlans returns the plans a guardian may buy, in display order.
func (handler billingHandler) listPlans(response http.ResponseWriter, request *http.Request) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "套餐暂时无法读取，请稍后重试")
		return
	}
	plans, err := handler.service.ListActivePlans(request.Context())
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	items := make([]planResponse, 0, len(plans))
	for _, plan := range plans {
		items = append(items, planResponseOf(plan))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"plans": items})
}

// listPlansGuardian serves the plan catalogue behind a family-scoped route.
//
// The catalogue is the same for every family, so the resolved family id is
// intentionally unused; it exists so the route runs after authentication and
// family authorization rather than exposing the catalogue publicly.
func (handler billingHandler) listPlansGuardian(
	response http.ResponseWriter,
	request *http.Request,
	_ string,
) {
	handler.listPlans(response, request)
}

// createOrder starts one guardian checkout for the authenticated family.
func (handler billingHandler) createOrder(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "下单暂时不可用，请稍后重试")
		return
	}
	var payload createBillingOrderRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择要购买的套餐")
		return
	}
	result, err := handler.service.CreateOrder(request.Context(), billingservice.CreateOrderRequest{
		ParentAccountID: parentAccountID,
		PlanCode:        payload.PlanCode,
		IdempotencyKey:  payload.IdempotencyKey,
		ConsentVersion:  payload.ConsentVersion,
		ClientIP:        clientIP(request),
	})
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	status := http.StatusCreated
	if result.Reused {
		status = http.StatusOK
	}
	writeSuccess(response, request, status, orderResponseOf(result.Order))
}

// listOrders returns the caller's own orders.
func (handler billingHandler) listOrders(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "订单暂时无法读取，请稍后重试")
		return
	}
	page, ok := billingPageOf(response, request)
	if !ok {
		return
	}
	orders, total, err := handler.service.ListOrdersForParent(request.Context(), parentAccountID, page)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"orders": orderResponses(orders),
		"total":  total,
	})
}

// getOrder returns one order of the calling family. Another family's order is
// answered exactly like a missing one.
func (handler billingHandler) getOrder(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "订单暂时无法读取，请稍后重试")
		return
	}
	order, err := handler.service.GetOrderForParent(
		request.Context(),
		parentAccountID,
		strings.TrimSpace(request.PathValue("order_no")),
	)
	if err != nil {
		writeBillingOrderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, orderResponseOf(*order))
}

// cancelOrder cancels a pending order of the calling family.
func (handler billingHandler) cancelOrder(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "订单暂时无法取消，请稍后重试")
		return
	}
	order, err := handler.service.CancelOrder(
		request.Context(),
		parentAccountID,
		strings.TrimSpace(request.PathValue("order_no")),
	)
	if err != nil {
		writeBillingOrderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, orderResponseOf(*order))
}

// payOrder opens one payment attempt for a pending order.
func (handler billingHandler) payOrder(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writePaymentNotOpen(response, request)
		return
	}
	var payload startBillingPaymentRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择支付方式")
		return
	}
	result, err := handler.service.StartPayment(
		request.Context(),
		parentAccountID,
		strings.TrimSpace(request.PathValue("order_no")),
		payload.Channel,
	)
	if err != nil {
		writeBillingOrderError(response, request, err)
		return
	}
	// The provider payload is public pre-payment data only; the merchant secret
	// never leaves the channel adapter.
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"payment_id":       result.Payment.ID,
		"channel":          string(result.Payment.Channel),
		"status":           string(result.Payment.Status),
		"amount_cny_cents": int64(result.Payment.AmountCents),
		"payload":          result.Payload,
	})
}

// requestRefund records a guardian refund request for a paid order.
func (handler billingHandler) requestRefund(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "退款暂时无法提交，请稍后重试")
		return
	}
	var payload requestBillingRefundRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请填写退款金额和原因")
		return
	}
	refund, err := handler.service.RequestRefund(request.Context(), billingservice.RequestRefundRequest{
		ParentAccountID: parentAccountID,
		OrderNo:         strings.TrimSpace(request.PathValue("order_no")),
		AmountCents:     billingdomain.Cents(payload.AmountCNYCents),
		Reason:          payload.Reason,
	})
	if err != nil {
		writeBillingOrderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, refundResponseOf(*refund))
}

// getConsent reports the family's live spending consent.
func (handler billingHandler) getConsent(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "消费授权暂时无法读取，请稍后重试")
		return
	}
	consent, err := handler.service.GetActiveConsent(request.Context(), parentAccountID, "")
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, consentResponseOf(consent))
}

// grantConsent records the guardian's agreement to the spending terms. An empty
// body is accepted: the client may confirm the current terms version without
// naming it, and the service defaults to the current version.
func (handler billingHandler) grantConsent(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "消费授权暂时无法保存，请稍后重试")
		return
	}
	payload := grantBillingConsentRequest{}
	if err := decodeJSON(request, &payload); err != nil && !errors.Is(err, io.EOF) {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请确认消费授权条款")
		return
	}
	consent, err := handler.service.GrantConsent(
		request.Context(),
		parentAccountID,
		payload.ConsentVersion,
		clientIP(request),
	)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, consentResponseOf(consent))
}

// revokeConsent withdraws the guardian's spending consent. A second revoke is
// not an error: the end state is the same.
func (handler billingHandler) revokeConsent(
	response http.ResponseWriter,
	request *http.Request,
	parentAccountID string,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "消费授权暂时无法撤回，请稍后重试")
		return
	}
	consentVersion := strings.TrimSpace(request.URL.Query().Get("consent_version"))
	revoked, err := handler.service.RevokeConsent(request.Context(), parentAccountID, consentVersion)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	if consentVersion == "" {
		consentVersion = billingservice.SpendingConsentVersion
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"consent_version": consentVersion,
		"revoked":         revoked,
		"is_active":       false,
	})
}

// --- payment provider callback --------------------------------------------

// billingNotificationMaxBytes bounds one provider callback body. A payment
// callback is a few kilobytes; the cap keeps a forged oversized request cheap.
const billingNotificationMaxBytes = 256 << 10

// receivePaymentNotification handles one provider callback.
//
// Raw body: the JSON is never parsed here. Signature verification hashes the
// exact bytes the provider signed, so the body is read once and passed through
// untouched; decoding it first would break the signature.
//
// Answers: once the service accepted the settlement or recognised a duplicate,
// the provider gets 200 so it stops retrying. A signature failure is 400, an
// unconfigured channel is 503, and an internal failure is 500 without detail.
func (handler billingHandler) receivePaymentNotification(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writePaymentNotOpen(response, request)
		return
	}
	channelName := strings.TrimSpace(request.PathValue("channel"))
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, billingNotificationMaxBytes))
	if err != nil {
		writeError(response, request, http.StatusRequestEntityTooLarge, "invalid_notification", "支付通知无法处理")
		return
	}
	outcome, err := handler.service.HandleNotification(
		request.Context(),
		channelName,
		newBillingNotificationRequest(request, body),
	)
	if err == nil {
		writeSuccess(response, request, http.StatusOK, map[string]any{
			"result":    string(outcome.Result),
			"duplicate": outcome.Duplicate,
		})
		return
	}
	// A duplicate is reported both as an outcome and as a sentinel; either
	// shape means the settlement already happened and must not be retried.
	if outcome.Duplicate || errors.Is(err, billingdomain.ErrDuplicateNotification) {
		writeSuccess(response, request, http.StatusOK, map[string]any{
			"result":    string(outcome.Result),
			"duplicate": true,
		})
		return
	}
	switch {
	case errors.Is(err, billingdomain.ErrSignatureInvalid):
		handler.logNotificationError(request, channelName, err)
		writeError(response, request, http.StatusBadRequest, "signature_invalid", "支付通知验签失败")
	case errors.Is(err, billingservice.ErrNotificationStillProcessing):
		// Telling the provider it succeeded would hide an unsettled payment.
		writeError(response, request, http.StatusServiceUnavailable, "notification_processing", "支付结果处理中，请稍后重试")
	case errors.Is(err, billingservice.ErrPaymentNotOpenYet):
		writePaymentNotOpen(response, request)
	default:
		handler.logNotificationError(request, channelName, err)
		writeError(response, request, http.StatusInternalServerError, "notification_failed", "支付通知处理失败")
	}
}

// newBillingNotificationRequest captures the provider's headers and bytes.
//
// Header keys are canonicalized so an adapter can look them up predictably, and
// a repeated header keeps its first value. The body is passed through byte for
// byte: its digest and signature must match what the provider sent.
func newBillingNotificationRequest(request *http.Request, body []byte) channel.NotificationRequest {
	headers := make(map[string]string, len(request.Header))
	for name, values := range request.Header {
		if len(values) == 0 {
			continue
		}
		headers[http.CanonicalHeaderKey(name)] = values[0]
	}
	return channel.NotificationRequest{
		Headers:    headers,
		RawBody:    body,
		ReceivedAt: time.Now().UTC(),
	}
}

// logNotificationError records a callback failure for operators. The body is
// never logged: it carries a provider receipt the platform must not retain.
func (handler billingHandler) logNotificationError(
	request *http.Request,
	channelName string,
	err error,
) {
	handler.logger.Error(
		"billing payment notification failed",
		"channel", channelName,
		"route", request.Method+" "+request.URL.Path,
		"error", err.Error(),
	)
}

// --- operator handlers -----------------------------------------------------

// adminListOrders returns a page of orders for the operator console.
func (handler billingHandler) adminListOrders(response http.ResponseWriter, request *http.Request) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "订单暂时无法读取，请稍后重试")
		return
	}
	page, ok := billingPageOf(response, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	filter := repository.OrderFilter{
		Status:          billingdomain.OrderStatus(strings.TrimSpace(query.Get("status"))),
		PlanCode:        strings.TrimSpace(query.Get("plan_code")),
		ParentAccountID: strings.TrimSpace(query.Get("parent_account_id")),
		Search:          strings.TrimSpace(query.Get("search")),
		Limit:           page.Limit,
		Offset:          page.Offset,
	}
	orders, total, err := handler.service.ListOrders(request.Context(), filter)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"orders": orderResponses(orders),
		"total":  total,
	})
}

// adminGetOrder returns one order addressed by an operator.
func (handler billingHandler) adminGetOrder(response http.ResponseWriter, request *http.Request) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "订单暂时无法读取，请稍后重试")
		return
	}
	order, err := handler.service.FindOrderByNumber(
		request.Context(),
		strings.TrimSpace(request.PathValue("order_no")),
	)
	if err != nil {
		writeBillingOrderError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, orderResponseOf(*order))
}

// adminListPlans returns a page of plans, including drafts and archived ones.
func (handler billingHandler) adminListPlans(response http.ResponseWriter, request *http.Request) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "套餐暂时无法读取，请稍后重试")
		return
	}
	page, ok := billingPageOf(response, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	plans, total, err := handler.service.ListPlans(
		request.Context(),
		billingdomain.PlanStatus(strings.TrimSpace(query.Get("status"))),
		query.Get("search"),
		page,
	)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	items := make([]planResponse, 0, len(plans))
	for _, plan := range plans {
		items = append(items, planResponseOf(plan))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"plans": items,
		"total": total,
	})
}

// adminCreatePlan inserts a plan, draft unless the operator publishes it.
func (handler billingHandler) adminCreatePlan(response http.ResponseWriter, request *http.Request) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "套餐暂时无法保存，请稍后重试")
		return
	}
	var payload billingPlanWriteRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查套餐信息")
		return
	}
	plan, err := handler.service.CreatePlan(request.Context(), billingPlanInputOf(payload))
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusCreated, planResponseOf(*plan))
}

// adminUpdatePlan saves a plan edit only when the client's version still
// matches, so two operators cannot silently overwrite each other's price.
func (handler billingHandler) adminUpdatePlan(response http.ResponseWriter, request *http.Request) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "套餐暂时无法保存，请稍后重试")
		return
	}
	var payload billingPlanWriteRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请检查套餐信息")
		return
	}
	expectedVersion, ok := billingPlanVersionOf(request, payload.Version)
	if !ok {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "套餐版本已过期，请刷新后重试")
		return
	}
	plan, err := handler.service.UpdatePlan(
		request.Context(),
		strings.TrimSpace(request.PathValue("plan_id")),
		expectedVersion,
		billingPlanInputOf(payload),
	)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, planResponseOf(*plan))
}

// adminApproveRefund approves a requested refund in the operator's name.
func (handler billingHandler) adminApproveRefund(response http.ResponseWriter, request *http.Request) {
	handler.decideRefund(response, request, true)
}

// adminRejectRefund rejects a refund that has not moved money yet.
func (handler billingHandler) adminRejectRefund(response http.ResponseWriter, request *http.Request) {
	handler.decideRefund(response, request, false)
}

// decideRefund shares the approve and reject path. The decision records the
// authenticated operator, because a refund without an approver is not auditable.
func (handler billingHandler) decideRefund(
	response http.ResponseWriter,
	request *http.Request,
	approve bool,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "退款暂时无法处理，请稍后重试")
		return
	}
	operatorAccountID, ok := authenticatedAccountID(request)
	if !ok {
		writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
		return
	}
	refundNo := strings.TrimSpace(request.PathValue("refund_no"))
	var refund *billingdomain.Refund
	var err error
	if approve {
		refund, err = handler.service.ApproveRefund(request.Context(), operatorAccountID, refundNo)
	} else {
		refund, err = handler.service.RejectRefund(request.Context(), operatorAccountID, refundNo)
	}
	if err != nil {
		writeBillingRefundError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, refundResponseOf(*refund))
}

// adminListReconciliation returns the recorded reconciliation runs.
func (handler billingHandler) adminListReconciliation(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "对账记录暂时无法读取，请稍后重试")
		return
	}
	page, ok := billingPageOf(response, request)
	if !ok {
		return
	}
	runs, err := handler.service.ListReconciliationRuns(request.Context(), page)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	items := make([]reconciliationRunResponse, 0, len(runs))
	for _, run := range runs {
		items = append(items, reconciliationRunResponseOf(run))
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{"runs": items})
}

// adminStartReconciliation compares one channel business day.
func (handler billingHandler) adminStartReconciliation(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "对账暂时无法发起，请稍后重试")
		return
	}
	var payload startBillingReconciliationRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择渠道和营业日")
		return
	}
	businessDate, err := time.Parse(time.DateOnly, strings.TrimSpace(payload.BusinessDate))
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请选择有效的营业日")
		return
	}
	run, err := handler.service.RunReconciliation(request.Context(), payload.Channel, businessDate)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	writeSuccess(response, request, http.StatusOK, reconciliationRunResponseOf(*run))
}

// adminGetReconciliation returns one run and its discrepancy items.
func (handler billingHandler) adminGetReconciliation(
	response http.ResponseWriter,
	request *http.Request,
) {
	if handler.service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "对账记录暂时无法读取，请稍后重试")
		return
	}
	runID := strings.TrimSpace(request.PathValue("run_id"))
	runs, err := handler.service.ListReconciliationRuns(
		request.Context(),
		billingservice.Page{Limit: billingservice.MaxPageSize},
	)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	var found *billingdomain.ReconciliationRun
	for index := range runs {
		if runs[index].ID == runID {
			found = &runs[index]
			break
		}
	}
	if found == nil {
		writeError(response, request, http.StatusNotFound, "reconciliation_not_found", "没有找到这次对账记录")
		return
	}
	items, err := handler.service.ListReconciliationItems(request.Context(), runID, false)
	if err != nil {
		writeBillingError(response, request, err)
		return
	}
	itemResponses := make([]reconciliationItemResponse, 0, len(items))
	for _, item := range items {
		itemResponses = append(itemResponses, reconciliationItemResponse{
			ItemID:           item.ID,
			ChannelTradeNo:   item.ChannelTradeNo,
			OrderNo:          item.OrderNo,
			ChannelCNYCents:  int64(item.ChannelCents),
			PlatformCNYCents: int64(item.PlatformCents),
			Discrepancy:      string(item.Discrepancy),
			Resolved:         item.Resolved,
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"run":   reconciliationRunResponseOf(*found),
		"items": itemResponses,
	})
}

// adminListChannels reports the registered payment channels and their health.
// Every provider is unconfigured this round, so each one shows unavailable and
// the configured count stays zero.
func (handler billingHandler) adminListChannels(response http.ResponseWriter, request *http.Request) {
	statuses := map[string]channel.HealthStatus{}
	if handler.channels != nil {
		statuses = handler.channels.Health(request.Context())
	}
	names := make([]string, 0, len(statuses))
	for name := range statuses {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]map[string]any, 0, len(names))
	configured := 0
	for _, name := range names {
		status := statuses[name]
		if status == channel.HealthStatusHealthy {
			configured++
		}
		items = append(items, map[string]any{
			"channel": name,
			"health":  string(status),
		})
	}
	writeSuccess(response, request, http.StatusOK, map[string]any{
		"channels":                 items,
		"registered_channel_count": len(names),
		"configured_channel_count": configured,
	})
}

// --- error mapping ---------------------------------------------------------

// writeBillingError maps a billing failure to a guardian-safe response.
//
// Validation and state-machine failures carry a message the guardian can act
// on. Internal causes (database, provider internals) collapse to a generic
// error so no schema or provider detail escapes.
func writeBillingError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, billingservice.ErrIdempotencyKeyRequired):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "请重新提交订单")
	case errors.Is(err, billingservice.ErrInvalidRequest),
		errors.Is(err, billingdomain.ErrInvalidAmount),
		errors.Is(err, billingdomain.ErrInvalidOrderAmounts),
		errors.Is(err, billingdomain.ErrInvalidRefundAmount):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "提交的信息不完整，请检查后重试")
	case errors.Is(err, billingdomain.ErrPlanNotFound),
		errors.Is(err, billingdomain.ErrOrderNotFound),
		errors.Is(err, repository.ErrRefundNotFound):
		writeError(response, request, http.StatusNotFound, "not_found", "没有找到对应的套餐或订单")
	case errors.Is(err, billingdomain.ErrPlanNotActive):
		writeError(response, request, http.StatusConflict, "plan_not_available", "该套餐已停止售卖，请选择其他套餐")
	case errors.Is(err, billingdomain.ErrSpendingConsentRequired):
		writeError(response, request, http.StatusConflict, "spending_consent_required", "请先由监护人确认消费授权")
	case errors.Is(err, billingservice.ErrPaymentNotOpenYet),
		errors.Is(err, billingdomain.ErrChannelNotConfigured):
		writePaymentNotOpen(response, request)
	case errors.Is(err, billingservice.ErrOrderNotCancellable):
		writeError(response, request, http.StatusConflict, "order_not_cancellable", "这笔订单已不能取消")
	case errors.Is(err, billingservice.ErrRefundNotRequestable),
		errors.Is(err, billingservice.ErrRefundNotCompletable),
		errors.Is(err, billingdomain.ErrOverRefund),
		errors.Is(err, billingdomain.ErrOrderNotPayable),
		errors.Is(err, billingdomain.ErrOrderExpired),
		errors.Is(err, billingdomain.ErrInvalidOrderTransition),
		errors.Is(err, repository.ErrVersionConflict):
		writeError(response, request, http.StatusConflict, "invalid_state", "当前状态不支持这个操作")
	case errors.Is(err, billingservice.ErrReconciliationUnavailable),
		errors.Is(err, billingservice.ErrStatementEmpty),
		errors.Is(err, billingservice.ErrStatementNotBalanced),
		errors.Is(err, repository.ErrRunNotFound),
		errors.Is(err, repository.ErrRunAlreadyExists):
		writeError(response, request, http.StatusServiceUnavailable, "reconciliation_unavailable", "对账数据暂时无法读取")
	default:
		writeError(response, request, http.StatusInternalServerError, "service_error", "操作没有完成，请稍后重试")
	}
}

// writeBillingOrderError maps a guardian order-path failure.
//
// A cross-family order is rendered exactly like a missing one: distinguishing
// them would let a caller enumerate other families' order numbers.
func writeBillingOrderError(response http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, billingdomain.ErrOrderNotFound) ||
		errors.Is(err, billingdomain.ErrCrossFamilyAccess) {
		writeError(response, request, http.StatusNotFound, "order_not_found", "没有找到这笔订单")
		return
	}
	writeBillingError(response, request, err)
}

// writeBillingRefundError maps a refund decision failure.
func writeBillingRefundError(response http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, billingdomain.ErrOrderNotFound) ||
		errors.Is(err, repository.ErrRefundNotFound) {
		writeError(response, request, http.StatusNotFound, "refund_not_found", "没有找到这笔退款")
		return
	}
	writeBillingError(response, request, err)
}

// writePaymentNotOpen renders the one message a guardian sees when no provider
// can collect money. It never mentions the provider or a missing credential.
func writePaymentNotOpen(response http.ResponseWriter, request *http.Request) {
	writeError(response, request, http.StatusServiceUnavailable, "payment_not_available", "支付功能尚未开放")
}

// --- helpers ---------------------------------------------------------------

// billingPageOf parses the shared limit and offset query parameters.
func billingPageOf(response http.ResponseWriter, request *http.Request) (billingservice.Page, bool) {
	limit, ok := optionalNonNegativeInt(request.URL.Query().Get("limit"))
	if !ok {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "每页数量不正确")
		return billingservice.Page{}, false
	}
	offset, ok := optionalNonNegativeInt(request.URL.Query().Get("offset"))
	if !ok {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "页码不正确")
		return billingservice.Page{}, false
	}
	return billingservice.Page{Limit: int(limit), Offset: int(offset)}, true
}

// optionalNonNegativeInt parses an optional non-negative integer.
func optionalNonNegativeInt(raw string) (int64, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, true
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

// billingPlanVersionOf resolves the optimistic-concurrency version from the
// body or an If-Match header. Reading the body first keeps one transport shape
// for clients that do not send headers.
func billingPlanVersionOf(request *http.Request, bodyVersion int) (int, bool) {
	if bodyVersion > 0 {
		return bodyVersion, true
	}
	headerVersion := strings.Trim(strings.TrimSpace(request.Header.Get("If-Match")), `"`)
	if headerVersion == "" || headerVersion == "*" {
		return 0, false
	}
	parsed, err := strconv.Atoi(headerVersion)
	if err != nil || parsed < 1 {
		return 0, false
	}
	return parsed, true
}

// clientIP returns the address the request came from for consent evidence.
// Proxy trust is already resolved by the transport middleware.
func clientIP(request *http.Request) string {
	return strings.TrimSpace(request.RemoteAddr)
}

// planResponseOf projects one plan onto the wire.
func planResponseOf(plan billingdomain.Plan) planResponse {
	return planResponse{
		PlanID:                plan.ID,
		Code:                  plan.Code,
		DisplayName:           plan.DisplayName,
		Description:           plan.Description,
		BillingPeriod:         string(plan.BillingPeriod),
		PeriodMonths:          plan.PeriodMonths,
		PriceCNYCents:         int64(plan.PriceCents),
		OriginalPriceCNYCents: int64(plan.OriginalPriceCents),
		AIQuotaUSD:            plan.AIQuotaUSD,
		Status:                string(plan.Status),
		SortOrder:             plan.SortOrder,
		Version:               plan.Version,
		PublishedAt:           plan.PublishedAt,
	}
}

// orderResponseOf projects one order onto the wire.
func orderResponseOf(order billingdomain.Order) orderResponse {
	return orderResponse{
		OrderNo:            order.OrderNo,
		PlanCode:           order.PlanCode,
		PlanDisplayName:    order.PlanDisplayName,
		BillingPeriod:      string(order.BillingPeriod),
		PeriodMonths:       order.PeriodMonths,
		UnitPriceCNYCents:  int64(order.UnitPriceCents),
		DiscountCNYCents:   int64(order.DiscountCents),
		PayableCNYCents:    int64(order.PayableCents),
		PaidCNYCents:       int64(order.PaidCents),
		RefundedCNYCents:   int64(order.RefundedCents),
		RefundableCNYCents: int64(order.RemainingRefundableCents()),
		Status:             string(order.Status),
		ExpiresAt:          order.ExpiresAt,
		PaidAt:             order.PaidAt,
		CancelledAt:        order.CancelledAt,
		CreatedAt:          order.CreatedAt,
	}
}

// orderResponses projects a page of orders onto the wire.
func orderResponses(orders []billingdomain.Order) []orderResponse {
	items := make([]orderResponse, 0, len(orders))
	for _, order := range orders {
		items = append(items, orderResponseOf(order))
	}
	return items
}

// refundResponseOf projects one refund onto the wire.
func refundResponseOf(refund billingdomain.Refund) refundResponse {
	return refundResponse{
		RefundNo:       refund.RefundNo,
		AmountCNYCents: int64(refund.AmountCents),
		Reason:         refund.Reason,
		Status:         string(refund.Status),
		RequestedAt:    refund.RequestedAt,
		ApprovedAt:     refund.ApprovedAt,
		CompletedAt:    refund.CompletedAt,
	}
}

// consentResponseOf reports the live consent, or the unconsented state when the
// family has none. No consent is a normal state, not an error.
func consentResponseOf(consent *billingdomain.SpendingConsent) consentResponse {
	if consent == nil {
		return consentResponse{
			ConsentVersion: billingservice.SpendingConsentVersion,
			IsActive:       false,
		}
	}
	grantedAt := consent.GrantedAt
	return consentResponse{
		ConsentVersion: consent.ConsentVersion,
		Method:         consent.Method,
		GrantedAt:      &grantedAt,
		RevokedAt:      consent.RevokedAt,
		IsActive:       consent.IsActive(),
	}
}

// reconciliationRunResponseOf projects one run onto the wire.
func reconciliationRunResponseOf(run billingdomain.ReconciliationRun) reconciliationRunResponse {
	return reconciliationRunResponse{
		RunID:                 run.ID,
		Channel:               string(run.Channel),
		BusinessDate:          run.BusinessDate.UTC().Format(time.DateOnly),
		Status:                string(run.Status),
		ChannelTotalCNYCents:  int64(run.ChannelTotalCents),
		PlatformTotalCNYCents: int64(run.PlatformTotalCents),
		DifferenceCNYCents:    int64(run.DifferenceCents),
		MatchedCount:          run.MatchedCount,
		MissingCount:          run.MissingCount,
		ExtraCount:            run.ExtraCount,
		ErrorMessage:          run.ErrorMessage,
		StartedAt:             run.StartedAt,
		FinishedAt:            run.FinishedAt,
	}
}

// billingPlanInputOf converts the wire request to the service input.
func billingPlanInputOf(payload billingPlanWriteRequest) billingservice.PlanInput {
	return billingservice.PlanInput{
		Code:               strings.TrimSpace(payload.Code),
		DisplayName:        payload.DisplayName,
		Description:        payload.Description,
		BillingPeriod:      billingdomain.BillingPeriod(strings.TrimSpace(payload.BillingPeriod)),
		PeriodMonths:       payload.PeriodMonths,
		PriceCents:         billingdomain.Cents(payload.PriceCNYCents),
		OriginalPriceCents: billingdomain.Cents(payload.OriginalPriceCNYCents),
		AIQuotaUSD:         payload.AIQuotaUSD,
		Status:             billingdomain.PlanStatus(strings.TrimSpace(payload.Status)),
		SortOrder:          payload.SortOrder,
	}
}

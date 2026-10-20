package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
)

// notificationPayload is the JSON body a scripted channel receives and verifies.
//
// A test builds this instead of hand-writing JSON so the callback contract lives
// in one place: a signature is computed over the exact bytes that are verified.
type notificationPayload struct {
	NotificationID string    `json:"notification_id"`
	ChannelTradeNo string    `json:"channel_trade_no"`
	OrderNo        string    `json:"order_no"`
	AmountCents    int64     `json:"amount_cents"`
	PaidAt         time.Time `json:"paid_at"`
	Success        bool      `json:"success"`
}

// scriptedChannel is a payment channel whose provider behaviour the test sets.
//
// It verifies signatures with a real HMACVerifier rather than a stub, so a test
// that proves "a forged callback credits nothing" exercises the production
// signature path, not a stand-in that simply returns an error when asked to.
type scriptedChannel struct {
	name      string
	verifier  channel.SignatureVerifier
	statement channel.Statement
	createErr error
	refundErr error
	fetchErr  error
	refundOK  bool
}

// testVerifier returns an HMAC verifier over the fixture secret.
func testVerifier(t *testing.T) *channel.HMACVerifier {
	t.Helper()
	verifier, err := channel.NewHMACVerifier("test-secret")
	if err != nil {
		t.Fatalf("build hmac verifier: %v", err)
	}
	return verifier
}

// unconfiguredChannel returns the production unconfigured-channel adapter, which
// is what a deployment without provider credentials actually registers.
func unconfiguredChannel(t *testing.T) channel.Channel {
	t.Helper()
	provider, err := channel.NewUnconfiguredChannel("wechat", channel.RejectAllVerifier{})
	if err != nil {
		t.Fatalf("build unconfigured channel: %v", err)
	}
	return provider
}

// newTestChannel returns a scripted channel named wechat that verifies with the
// given verifier.
func newTestChannel(t *testing.T, verifier *channel.HMACVerifier) *scriptedChannel {
	t.Helper()
	if verifier == nil {
		t.Fatal("test channel requires a verifier")
	}
	return &scriptedChannel{name: "wechat", verifier: verifier, refundOK: true}
}

// newScriptedChannel returns a channel that accepts nothing, for reconciliation
// tests that only need a statement.
func newScriptedChannel() *scriptedChannel {
	return &scriptedChannel{name: "wechat", verifier: channel.RejectAllVerifier{}, refundOK: true}
}

// Name returns the channel key.
func (c *scriptedChannel) Name() string { return c.name }

// CreatePayment returns a deterministic pre-payment payload.
func (c *scriptedChannel) CreatePayment(
	ctx context.Context,
	request channel.CreatePaymentRequest,
) (channel.CreatePaymentResult, error) {
	if c.createErr != nil {
		return channel.CreatePaymentResult{}, c.createErr
	}
	if request.AmountCents <= 0 {
		return channel.CreatePaymentResult{}, errors.New("scripted channel: amount must be positive")
	}
	return channel.CreatePaymentResult{
		ChannelTradeNo: "trade-" + request.OrderNo,
		Payload:        map[string]string{"prepay_id": "prepay-" + request.OrderNo},
	}, nil
}

// VerifyNotification checks the signature and maps the body to an event.
func (c *scriptedChannel) VerifyNotification(
	ctx context.Context,
	request channel.NotificationRequest,
) (channel.NotificationEvent, error) {
	header := http.Header{}
	for key, value := range request.Headers {
		header.Set(key, value)
	}
	var payload notificationPayload
	// The body is parsed before verification only to fill the event on a
	// rejection; nothing is acted on unless Verify succeeds.
	parseErr := json.Unmarshal(request.RawBody, &payload)
	if verifyErr := c.verifier.Verify(header, request.RawBody); verifyErr != nil {
		return channel.NotificationEvent{
			NotificationID: payload.NotificationID,
			ChannelTradeNo: payload.ChannelTradeNo,
			OrderNo:        payload.OrderNo,
		}, verifyErr
	}
	if parseErr != nil {
		return channel.NotificationEvent{}, parseErr
	}
	return channel.NotificationEvent{
		NotificationID: payload.NotificationID,
		ChannelTradeNo: payload.ChannelTradeNo,
		OrderNo:        payload.OrderNo,
		AmountCents:    payload.AmountCents,
		PaidAt:         payload.PaidAt,
		Success:        payload.Success,
	}, nil
}

// Refund reports the scripted outcome.
func (c *scriptedChannel) Refund(
	ctx context.Context,
	request channel.RefundRequest,
) (channel.RefundResult, error) {
	if c.refundErr != nil {
		return channel.RefundResult{}, c.refundErr
	}
	if request.AmountCents <= 0 {
		return channel.RefundResult{}, errors.New("scripted channel: refund must be positive")
	}
	return channel.RefundResult{
		ChannelRefundNo: "refund-" + request.RefundNo,
		Succeeded:       c.refundOK,
	}, nil
}

// FetchStatement returns the scripted statement.
func (c *scriptedChannel) FetchStatement(
	ctx context.Context,
	businessDate time.Time,
) (channel.Statement, error) {
	if c.fetchErr != nil {
		return channel.Statement{}, c.fetchErr
	}
	statement := c.statement
	statement.BusinessDate = businessDate
	return statement, nil
}

// HealthCheck reports healthy for the scripted channel.
func (c *scriptedChannel) HealthCheck(ctx context.Context) (channel.HealthStatus, error) {
	return channel.HealthStatusHealthy, nil
}

// signedNotification builds a callback body and signs the exact bytes sent.
//
// The signing secret is passed separately so a test can sign with the wrong one
// and prove the handler refuses it.
func signedNotification(
	t *testing.T,
	signer *channel.HMACVerifier,
	secret string,
	payload notificationPayload,
) channel.NotificationRequest {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal notification payload: %v", err)
	}
	signingVerifier := signer
	if secret != "test-secret" {
		signingVerifier, err = channel.NewHMACVerifier(secret)
		if err != nil {
			t.Fatalf("build signing verifier: %v", err)
		}
	}
	signature, err := signingVerifier.SignHex(body)
	if err != nil {
		t.Fatalf("sign notification: %v", err)
	}
	return channel.NotificationRequest{
		Headers:    map[string]string{strings.ToLower(channel.HMACHeaderName): signature},
		RawBody:    body,
		ReceivedAt: time.Now().UTC(),
	}
}

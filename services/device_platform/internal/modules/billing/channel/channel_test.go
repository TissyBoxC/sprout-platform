package channel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSecret = "test-shared-secret-not-used-in-production"

// testContext bounds every channel call so a hang fails the test instead of
// blocking the suite.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func newTestUnconfiguredChannel(t *testing.T, name string, verifier SignatureVerifier) *UnconfiguredChannel {
	t.Helper()
	channel, err := NewUnconfiguredChannel(name, verifier)
	if err != nil {
		t.Fatalf("NewUnconfiguredChannel(%q) returned error: %v", name, err)
	}
	return channel
}

// testParser mimics the provider decoder that a real integration supplies: it
// only echoes fields from the raw body, proving the verifier gate is what lets
// it run.
func testParser(request NotificationRequest) (NotificationEvent, error) {
	fields := strings.Split(string(request.RawBody), "|")
	if len(fields) < 4 {
		return NotificationEvent{}, errors.New("malformed test callback body")
	}
	amount := int64(0)
	for _, character := range fields[2] {
		if character < '0' || character > '9' {
			return NotificationEvent{}, errors.New("malformed amount")
		}
		amount = amount*10 + int64(character-'0')
	}
	return NotificationEvent{
		NotificationID: fields[0],
		OrderNo:        fields[1],
		ChannelTradeNo: fields[3],
		AmountCents:    amount,
		PaidAt:         request.ReceivedAt,
		Success:        true,
	}, nil
}

func signedRequest(t *testing.T, verifier *HMACVerifier, body string) NotificationRequest {
	t.Helper()
	signature, err := verifier.SignHex([]byte(body))
	if err != nil {
		t.Fatalf("SignHex returned error: %v", err)
	}
	return NotificationRequest{
		Headers:    map[string]string{HMACHeaderName: signature},
		RawBody:    []byte(body),
		ReceivedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewUnconfiguredChannelRejectsUnknownName(t *testing.T) {
	for _, name := range []string{"", "paypal", "manual", " wechat pay "} {
		if _, err := NewUnconfiguredChannel(name, nil); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("NewUnconfiguredChannel(%q) error = %v, want ErrInvalidRequest", name, err)
		}
	}
}

func TestNewUnconfiguredChannelNormalizesName(t *testing.T) {
	channel := newTestUnconfiguredChannel(t, "  WeChat ", nil)
	if got := channel.Name(); got != "wechat" {
		t.Fatalf("Name() = %q, want %q", got, "wechat")
	}
}

func TestNewUnconfiguredChannelsCoversSupportedNames(t *testing.T) {
	channels := NewUnconfiguredChannels()
	if len(channels) != len(SupportedChannelNames) {
		t.Fatalf("NewUnconfiguredChannels() length = %d, want %d", len(channels), len(SupportedChannelNames))
	}
	seen := make(map[string]bool, len(channels))
	for _, channel := range channels {
		seen[channel.Name()] = true
	}
	for _, name := range SupportedChannelNames {
		if !seen[name] {
			t.Fatalf("expected placeholder channel %q", name)
		}
	}
}

func TestUnconfiguredChannelInterfaceContract(t *testing.T) {
	// Compile-time and runtime proof that the placeholder satisfies Channel.
	// Only the pointer type is a Channel: the zero value is not usable because the
	// name and verifier are set by the constructor.
	var _ Channel = (*UnconfiguredChannel)(nil)

	for _, name := range []string{"wechat", "alipay"} {
		channel := newTestUnconfiguredChannel(t, name, nil)
		var channelInterface Channel = channel
		if channelInterface.Name() != name {
			t.Fatalf("Name() = %q, want %q", channelInterface.Name(), name)
		}
	}
}

func TestUnconfiguredChannelOutboundCallsFailLoudly(t *testing.T) {
	ctx := testContext(t)
	channel := newTestUnconfiguredChannel(t, "wechat", nil)
	expiresAt := time.Now().Add(30 * time.Minute).UTC()

	payment, err := channel.CreatePayment(ctx, CreatePaymentRequest{
		OrderNo:     "SP20261020A1B2C3",
		AmountCents: 990,
		Subject:     "初芽体验包",
		ClientIP:    "203.0.113.7",
		NotifyURL:   "https://example.invalid/api/v1/billing/notifications/wechat",
		ExpiresAt:   expiresAt,
	})
	if !errors.Is(err, ErrChannelNotConfigured) {
		t.Fatalf("CreatePayment error = %v, want wrapping ErrChannelNotConfigured", err)
	}
	if payment.ChannelTradeNo != "" || payment.Payload != nil {
		t.Fatalf("CreatePayment returned a non-empty result on failure: %+v", payment)
	}

	refund, err := channel.Refund(ctx, RefundRequest{
		OrderNo:        "SP20261020A1B2C3",
		ChannelTradeNo: "4200000000",
		RefundNo:       "RF20261020D4E5F6",
		AmountCents:    990,
		Reason:         "家长申请退款",
	})
	if !errors.Is(err, ErrChannelNotConfigured) {
		t.Fatalf("Refund error = %v, want wrapping ErrChannelNotConfigured", err)
	}
	if refund.ChannelRefundNo != "" || refund.Succeeded {
		t.Fatalf("Refund returned a success-looking result on failure: %+v", refund)
	}

	statement, err := channel.FetchStatement(ctx, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrChannelNotConfigured) {
		t.Fatalf("FetchStatement error = %v, want wrapping ErrChannelNotConfigured", err)
	}
	if statement.Entries != nil || !statement.BusinessDate.IsZero() {
		t.Fatalf("FetchStatement returned a non-empty statement on failure: %+v", statement)
	}
}

func TestUnconfiguredChannelOutboundErrorNamesChannel(t *testing.T) {
	ctx := testContext(t)
	channel := newTestUnconfiguredChannel(t, "alipay", nil)
	_, err := channel.CreatePayment(ctx, CreatePaymentRequest{OrderNo: "SP1", AmountCents: 100})
	if err == nil {
		t.Fatal("CreatePayment returned nil error")
	}
	// Operators need to identify which provider is missing; the message carries
	// no credential material.
	if !strings.Contains(err.Error(), "alipay") {
		t.Fatalf("error %q does not name the channel", err.Error())
	}
}

func TestUnconfiguredChannelOutboundHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	channel := newTestUnconfiguredChannel(t, "wechat", nil)
	_, err := channel.FetchStatement(ctx, time.Now())
	if !errors.Is(err, ErrChannelNotConfigured) || !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchStatement error = %v, want ErrChannelNotConfigured and context.Canceled", err)
	}
}

func TestUnconfiguredChannelVerifyNotificationFailsClosedByDefault(t *testing.T) {
	ctx := testContext(t)
	channel := newTestUnconfiguredChannel(t, "wechat", nil)
	request := NotificationRequest{
		Headers:    map[string]string{HMACHeaderName: "anything"},
		RawBody:    []byte(`{"out_trade_no":"SP1","amount":990}`),
		ReceivedAt: time.Now().UTC(),
	}

	event, err := channel.VerifyNotification(ctx, request)
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("VerifyNotification error = %v, want wrapping ErrSignatureInvalid", err)
	}
	// The same failure must also identify the missing configuration so the
	// Feature Center can show "not configured" rather than "under attack".
	if !errors.Is(err, ErrChannelNotConfigured) {
		t.Fatalf("VerifyNotification error = %v, want wrapping ErrChannelNotConfigured", err)
	}
	if event != (NotificationEvent{}) {
		t.Fatalf("VerifyNotification returned event %+v on failure", event)
	}
}

func TestUnconfiguredChannelVerifyNotificationRejectsEveryShape(t *testing.T) {
	ctx := testContext(t)
	channel := newTestUnconfiguredChannel(t, "alipay", nil)
	cases := map[string]NotificationRequest{
		"empty request":  {},
		"empty body":     {Headers: map[string]string{HMACHeaderName: "abc"}, RawBody: []byte{}},
		"no headers":     {RawBody: []byte(`{"a":1}`)},
		"plausible body": {Headers: map[string]string{HMACHeaderName: strings.Repeat("0", 64)}, RawBody: []byte(`{"trade_status":"TRADE_SUCCESS"}`)},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := channel.VerifyNotification(ctx, request)
			if !errors.Is(err, ErrSignatureInvalid) {
				t.Fatalf("VerifyNotification error = %v, want ErrSignatureInvalid", err)
			}
		})
	}
}

func TestUnconfiguredChannelVerifyNotificationRequiresParserAfterSignature(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	channel := newTestUnconfiguredChannel(t, "wechat", verifier)
	request := signedRequest(t, verifier, "NTF-1|SP1|990|4200000000")

	// A valid signature proves authenticity, not meaning: without a provider
	// decoder the channel must refuse rather than emit a zero-value event.
	_, err = channel.VerifyNotification(testContext(t), request)
	if !errors.Is(err, ErrChannelNotConfigured) {
		t.Fatalf("VerifyNotification error = %v, want ErrChannelNotConfigured", err)
	}
}

func TestUnconfiguredChannelVerifyNotificationWithParser(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	channel := newTestUnconfiguredChannel(t, "wechat", verifier).WithNotificationParser(testParser)
	request := signedRequest(t, verifier, "NTF-1|SP1|990|4200000000")

	event, err := channel.VerifyNotification(testContext(t), request)
	if err != nil {
		t.Fatalf("VerifyNotification returned error: %v", err)
	}
	want := NotificationEvent{
		NotificationID: "NTF-1",
		OrderNo:        "SP1",
		ChannelTradeNo: "4200000000",
		AmountCents:    990,
		PaidAt:         request.ReceivedAt,
		Success:        true,
	}
	if event != want {
		t.Fatalf("event = %+v, want %+v", event, want)
	}
}

func TestUnconfiguredChannelVerifyNotificationRejectsTamperedBodyWithParser(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	channel := newTestUnconfiguredChannel(t, "wechat", verifier).WithNotificationParser(testParser)
	request := signedRequest(t, verifier, "NTF-1|SP1|990|4200000000")
	// Flip the amount after signing: the classic "pay 1 fen, claim 990" attack.
	request.RawBody = []byte("NTF-1|SP1|1|4200000000")

	event, err := channel.VerifyNotification(testContext(t), request)
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("VerifyNotification error = %v, want ErrSignatureInvalid", err)
	}
	if event != (NotificationEvent{}) {
		t.Fatalf("tampered callback produced event %+v", event)
	}
}

func TestUnconfiguredChannelVerifyNotificationRejectsUnusableEvent(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	channel := newTestUnconfiguredChannel(t, "wechat", verifier)
	channel.WithNotificationParser(func(NotificationRequest) (NotificationEvent, error) {
		return NotificationEvent{OrderNo: "SP1"}, nil
	})
	request := signedRequest(t, verifier, "body")

	// Missing notification_id would break the idempotency ledger key.
	if _, err := channel.VerifyNotification(testContext(t), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("VerifyNotification error = %v, want ErrInvalidRequest", err)
	}
}

func TestUnconfiguredChannelVerifyNotificationStopsWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	channel := newTestUnconfiguredChannel(t, "wechat", verifier).WithNotificationParser(testParser)
	request := signedRequest(t, verifier, "NTF-1|SP1|990|4200000000")

	if _, err := channel.VerifyNotification(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyNotification error = %v, want context.Canceled", err)
	}
}

func TestUnconfiguredChannelHealthCheckIsAlwaysUnavailable(t *testing.T) {
	ctx := testContext(t)
	for _, name := range SupportedChannelNames {
		for _, verifier := range []SignatureVerifier{nil, RejectAllVerifier{}} {
			channel := newTestUnconfiguredChannel(t, name, verifier)
			status, err := channel.HealthCheck(ctx)
			if status != HealthStatusUnavailable {
				t.Fatalf("%s HealthCheck status = %q, want %q", name, status, HealthStatusUnavailable)
			}
			// The error explains why; a silent unavailable is fine for Feature
			// Center but operators still get a reason in logs.
			if err == nil {
				t.Fatalf("%s HealthCheck returned nil error", name)
			}
		}
	}
}

func TestUnconfiguredChannelHealthCheckIgnoresCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	channel := newTestUnconfiguredChannel(t, "alipay", nil)
	status, _ := channel.HealthCheck(ctx)
	if status != HealthStatusUnavailable {
		t.Fatalf("HealthCheck status = %q, want %q", status, HealthStatusUnavailable)
	}
}

func TestHealthStatusIsKnownAndStable(t *testing.T) {
	cases := map[HealthStatus]bool{
		HealthStatusHealthy:     true,
		HealthStatusDegraded:    true,
		HealthStatusUnavailable: true,
		"":                      false,
		"ok":                    false,
		"HEALTHY":               false,
	}
	for status, wantKnown := range cases {
		if got := status.IsKnown(); got != wantKnown {
			t.Fatalf("%q IsKnown() = %v, want %v", status, got, wantKnown)
		}
	}
	if HealthStatusHealthy.String() != "healthy" || HealthStatusUnavailable.String() != "unavailable" {
		t.Fatal("HealthStatus wire values changed; Feature Center depends on them")
	}
}

func TestRejectAllVerifierRejectsEverything(t *testing.T) {
	verifier := RejectAllVerifier{}
	secret := "whatever"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("body"))
	validLooking := map[string]string{HMACHeaderName: hex.EncodeToString(mac.Sum(nil))}

	headers := []http.Header{nil, {}, headerFromFlat(validLooking), {HMACHeaderName: []string{""}}}
	for index, header := range headers {
		if err := verifier.Verify(header, []byte("body")); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("case %d: Verify error = %v, want ErrSignatureInvalid", index, err)
		} else if !errors.Is(err, ErrChannelNotConfigured) {
			t.Fatalf("case %d: Verify error = %v, want ErrChannelNotConfigured", index, err)
		}
	}
	if err := verifier.Verify(nil, nil); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Verify(nil, nil) error = %v, want ErrSignatureInvalid", err)
	}
}

// headerFromFlat builds an http.Header for the table above.
func headerFromFlat(headers map[string]string) http.Header {
	converted := make(http.Header, len(headers))
	for key, value := range headers {
		converted[key] = []string{value}
	}
	return converted
}

func TestNewHMACVerifierRejectsEmptySecret(t *testing.T) {
	for _, secret := range []string{"", "   ", "\t\n"} {
		verifier, err := NewHMACVerifier(secret)
		if err == nil {
			t.Fatalf("NewHMACVerifier(%q) returned no error", secret)
		}
		if verifier != nil {
			t.Fatalf("NewHMACVerifier(%q) returned a verifier on error", secret)
		}
		if !errors.Is(err, ErrChannelNotConfigured) {
			t.Fatalf("NewHMACVerifier(%q) error = %v, want ErrChannelNotConfigured", secret, err)
		}
		// The secret must never leak into an error message.
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("error message echoes the secret: %q", err.Error())
		}
	}
}

func TestZeroValueHMACVerifierFailsClosed(t *testing.T) {
	verifier := &HMACVerifier{}
	header := http.Header{HMACHeaderName: []string{strings.Repeat("a", 64)}}
	err := verifier.Verify(header, []byte("body"))
	if !errors.Is(err, ErrSignatureInvalid) || !errors.Is(err, ErrChannelNotConfigured) {
		t.Fatalf("zero-value Verify error = %v, want signature + not-configured", err)
	}
	if _, signErr := verifier.Sign([]byte("body")); signErr == nil {
		t.Fatal("zero-value Sign returned no error")
	}
}

func TestHMACVerifierAcceptsValidSignature(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	body := []byte(`{"out_trade_no":"SP1","amount_cny_cents":990,"trade_status":"TRADE_SUCCESS"}`)
	signature, err := verifier.SignHex(body)
	if err != nil {
		t.Fatalf("SignHex returned error: %v", err)
	}
	// Header lookup must tolerate any key casing: a callback handler that builds
	// the map by hand must not silently reject a valid provider signature.
	for _, headerName := range []string{HMACHeaderName, "x-signature", "X-SIGNATURE", "X-signature"} {
		header := http.Header{headerName: []string{signature}}
		if err := verifier.Verify(header, body); err != nil {
			t.Fatalf("Verify with header %q returned error: %v", headerName, err)
		}
	}
	if err := verifier.Verify(httpHeaderFromFlat(map[string]string{"x-signature": signature}), body); err != nil {
		t.Fatalf("Verify via httpHeaderFromFlat returned error: %v", err)
	}
}

func TestHMACVerifierAcceptsBase64Signature(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	body := []byte("body")
	sum, err := verifier.Sign(body)
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}
	header := http.Header{HMACHeaderName: []string{base64.StdEncoding.EncodeToString(sum)}}
	if err := verifier.Verify(header, body); err != nil {
		t.Fatalf("Verify with base64 signature returned error: %v", err)
	}
}

func TestHMACVerifierRejectsTamperedBody(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	body := []byte(`{"amount_cny_cents":990}`)
	signature, err := verifier.SignHex(body)
	if err != nil {
		t.Fatalf("SignHex returned error: %v", err)
	}
	header := http.Header{HMACHeaderName: []string{signature}}

	tampered := [][]byte{
		[]byte(`{"amount_cny_cents":1}`),
		[]byte(`{"amount_cny_cents":990} `), // trailing space matters
		append(append([]byte{}, body...), 'x'),
		body[:len(body)-1],
		{},
		nil,
	}
	for index, candidate := range tampered {
		if err := verifier.Verify(header, candidate); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("tampered case %d: Verify error = %v, want ErrSignatureInvalid", index, err)
		}
	}
}

func TestHMACVerifierRejectsWrongSecret(t *testing.T) {
	signer, err := NewHMACVerifier("secret-one")
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	checker, err := NewHMACVerifier("secret-two")
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	body := []byte("body")
	signature, err := signer.SignHex(body)
	if err != nil {
		t.Fatalf("SignHex returned error: %v", err)
	}
	header := http.Header{HMACHeaderName: []string{signature}}
	if err := checker.Verify(header, body); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Verify with wrong secret error = %v, want ErrSignatureInvalid", err)
	}
}

func TestHMACVerifierRejectsMalformedHeaders(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	cases := map[string]http.Header{
		"missing header":    {},
		"empty header":      {HMACHeaderName: []string{""}},
		"whitespace header": {HMACHeaderName: []string{"   "}},
		"short hex":         {HMACHeaderName: []string{"abcd"}},
		"odd hex":           {HMACHeaderName: []string{"abc"}},
		"garbage":           {HMACHeaderName: []string{"not-a-signature"}},
		"wrong length hex":  {HMACHeaderName: []string{strings.Repeat("0", 64)}},
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			err := verifier.Verify(header, []byte("body"))
			if !errors.Is(err, ErrSignatureInvalid) {
				t.Fatalf("Verify error = %v, want ErrSignatureInvalid", err)
			}
		})
	}
}

func TestHMACVerifierUsesConstantTimeCompare(t *testing.T) {
	// The verification path must call crypto/subtle.ConstantTimeCompare rather
	// than == / bytes.Equal, which short-circuit and leak prefix length. Go has
	// no hook that observes which compare a call made, so the source text is the
	// honest witness for that implementation detail.
	source, err := os.ReadFile("signature.go")
	if err != nil {
		t.Fatalf("read signature.go: %v", err)
	}
	if !strings.Contains(string(source), "subtle.ConstantTimeCompare") {
		t.Fatal("signature.go no longer uses subtle.ConstantTimeCompare")
	}
	// Non-vacuous guard on the behavioural half: a near-miss must be rejected
	// just like a far-miss.
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	body := []byte("body")
	signature, err := verifier.SignHex(body)
	if err != nil {
		t.Fatalf("SignHex returned error: %v", err)
	}
	nearMiss := "0" + signature[1:]
	if nearMiss == signature {
		nearMiss = "1" + signature[1:]
	}
	nearErr := verifier.Verify(http.Header{HMACHeaderName: []string{nearMiss}}, body)
	farErr := verifier.Verify(http.Header{HMACHeaderName: []string{strings.Repeat("f", 64)}}, body)
	if nearErr == nil || farErr == nil {
		t.Fatalf("nearErr = %v, farErr = %v, want both rejected", nearErr, farErr)
	}
}

func TestHMACVerifierSignatureIsDeterministic(t *testing.T) {
	verifier, err := NewHMACVerifier(testSecret)
	if err != nil {
		t.Fatalf("NewHMACVerifier returned error: %v", err)
	}
	body := []byte("body")
	first, err := verifier.SignHex(body)
	if err != nil {
		t.Fatalf("SignHex returned error: %v", err)
	}
	second, err := verifier.SignHex(body)
	if err != nil {
		t.Fatalf("SignHex returned error: %v", err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("SignHex not stable: %q vs %q", first, second)
	}
	reference := hmac.New(sha256.New, []byte(testSecret))
	reference.Write(body)
	if want := hex.EncodeToString(reference.Sum(nil)); first != want {
		t.Fatalf("SignHex = %q, want standard HMAC-SHA256 %q", first, want)
	}
}

func TestNotificationDigest(t *testing.T) {
	first := []byte(`{"notification_id":"NTF-1","amount_cny_cents":990}`)
	second := []byte(`{"notification_id":"NTF-1","amount_cny_cents":1}`)

	digest := NotificationDigest(first)
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest %q lacks sha256: prefix", digest)
	}
	if len(digest) != len("sha256:")+64 {
		t.Fatalf("digest %q is not sha256 hex", digest)
	}
	if NotificationDigest(first) != digest {
		t.Fatal("digest is not stable for the same body")
	}
	if NotificationDigest(second) == digest {
		t.Fatal("digest is identical for different bodies")
	}
	if NotificationDigest(nil) != NotificationDigest([]byte{}) {
		t.Fatal("nil and empty bodies should share the empty digest")
	}
}

func TestNotificationDigestMatchesSHA256(t *testing.T) {
	body := []byte(`{"a":1}`)
	sum := sha256.Sum256(body)
	if want := "sha256:" + hex.EncodeToString(sum[:]); NotificationDigest(body) != want {
		t.Fatalf("NotificationDigest = %q, want %q", NotificationDigest(body), want)
	}
}

func TestIsDuplicateNotification(t *testing.T) {
	if IsDuplicateNotification(nil) {
		t.Fatal("nil error reported as duplicate")
	}
	if IsDuplicateNotification(ErrSignatureInvalid) {
		t.Fatal("signature error reported as duplicate")
	}
	if !IsDuplicateNotification(fmtWrap(ErrDuplicateNotification)) {
		t.Fatal("wrapped ErrDuplicateNotification not detected")
	}
}

// fmtWrap provides a wrapped sentinel without importing fmt in every test.
func fmtWrap(sentinel error) error {
	return &wrappedError{inner: sentinel}
}

type wrappedError struct{ inner error }

func (err *wrappedError) Error() string { return "callback rejected: " + err.inner.Error() }
func (err *wrappedError) Unwrap() error { return err.inner }

func TestRegistryRegisterAndGet(t *testing.T) {
	registry := NewRegistry()
	wechat := newTestUnconfiguredChannel(t, "wechat", nil)
	alipay := newTestUnconfiguredChannel(t, "alipay", nil)

	if err := registry.Register(wechat); err != nil {
		t.Fatalf("Register(wechat) returned error: %v", err)
	}
	if err := registry.Register(alipay); err != nil {
		t.Fatalf("Register(alipay) returned error: %v", err)
	}

	got, exists := registry.Get("wechat")
	if !exists || got.Name() != "wechat" {
		t.Fatalf("Get(wechat) = %v, %v", got, exists)
	}
	// Lookup is case-insensitive because channel names arrive from requests.
	if got, exists := registry.Get("  ALIPAY "); !exists || got.Name() != "alipay" {
		t.Fatalf("Get(  ALIPAY ) = %v, %v", got, exists)
	}
	if got, exists := registry.Get("manual"); exists || got != nil {
		t.Fatalf("Get(manual) = %v, %v, want nil, false", got, exists)
	}
	if got, exists := registry.Get(""); exists || got != nil {
		t.Fatalf("Get(\"\") = %v, %v, want nil, false", got, exists)
	}
}

func TestRegistryRejectsDuplicateName(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(newTestUnconfiguredChannel(t, "wechat", nil)); err != nil {
		t.Fatalf("first Register returned error: %v", err)
	}
	// A differently-cased duplicate must still be rejected: both would answer
	// callbacks for the same provider.
	err := registry.Register(newTestUnconfiguredChannel(t, "WeChat", nil))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("duplicate Register error = %v, want ErrInvalidRequest", err)
	}
	if names := registry.Names(); len(names) != 1 {
		t.Fatalf("Names() = %v, want one entry", names)
	}
}

func TestRegistryRejectsNilAndUnnamedChannels(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Register(nil) error = %v, want ErrInvalidRequest", err)
	}
	// A typed nil pointer is still non-nil as an interface; the name check below
	// is what catches a channel that was never initialized.
	var nilChannel *UnconfiguredChannel
	if err := registry.Register(nilChannel); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Register(typed nil) error = %v, want ErrInvalidRequest", err)
	}
	if err := registry.Register(unnamedChannel{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Register(unnamed) error = %v, want ErrInvalidRequest", err)
	}
	if names := registry.Names(); len(names) != 0 {
		t.Fatalf("Names() = %v, want empty", names)
	}
}

// unnamedChannel has an empty Name and exercises the registry's guard.
type unnamedChannel struct{}

func (unnamedChannel) Name() string { return "  " }
func (unnamedChannel) CreatePayment(context.Context, CreatePaymentRequest) (CreatePaymentResult, error) {
	return CreatePaymentResult{}, ErrChannelNotConfigured
}
func (unnamedChannel) VerifyNotification(context.Context, NotificationRequest) (NotificationEvent, error) {
	return NotificationEvent{}, ErrSignatureInvalid
}
func (unnamedChannel) Refund(context.Context, RefundRequest) (RefundResult, error) {
	return RefundResult{}, ErrChannelNotConfigured
}
func (unnamedChannel) FetchStatement(context.Context, time.Time) (Statement, error) {
	return Statement{}, ErrChannelNotConfigured
}
func (unnamedChannel) HealthCheck(context.Context) (HealthStatus, error) {
	return HealthStatusUnavailable, ErrChannelNotConfigured
}

func TestRegistryNamesAreSorted(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"wechat", "alipay"} {
		if err := registry.Register(newTestUnconfiguredChannel(t, name, nil)); err != nil {
			t.Fatalf("Register(%s) returned error: %v", name, err)
		}
	}
	// Register in a non-alphabetical order; Names must still sort.
	names := registry.Names()
	if len(names) != 2 || names[0] != "alipay" || names[1] != "wechat" {
		t.Fatalf("Names() = %v, want [alipay wechat]", names)
	}
	if empty := NewRegistry().Names(); empty == nil || len(empty) != 0 {
		t.Fatalf("empty registry Names() = %v, want empty non-nil slice", empty)
	}
}

func TestRegistryHealthReportsUnavailable(t *testing.T) {
	registry := NewRegistry()
	for _, channel := range NewUnconfiguredChannels() {
		if err := registry.Register(channel); err != nil {
			t.Fatalf("Register(%s) returned error: %v", channel.Name(), err)
		}
	}
	health := registry.Health(testContext(t))
	if len(health) != len(SupportedChannelNames) {
		t.Fatalf("Health() = %v, want %d entries", health, len(SupportedChannelNames))
	}
	for _, name := range SupportedChannelNames {
		status, exists := health[name]
		if !exists {
			t.Fatalf("Health() missing %q", name)
		}
		if status != HealthStatusUnavailable {
			t.Fatalf("Health()[%q] = %q, want %q", name, status, HealthStatusUnavailable)
		}
	}
}

func TestRegistryHealthDegradesErrorsAndUnknownStatuses(t *testing.T) {
	registry := NewRegistry()
	for _, channel := range []Channel{
		stubChannel{name: "healthy", status: HealthStatusHealthy},
		stubChannel{name: "degraded", status: HealthStatusDegraded},
		stubChannel{name: "errored", status: HealthStatusHealthy, healthError: ErrChannelNotConfigured},
		stubChannel{name: "unknown", status: HealthStatus("ok")},
		stubChannel{name: "empty", status: ""},
	} {
		if err := registry.Register(channel); err != nil {
			t.Fatalf("Register(%s) returned error: %v", channel.Name(), err)
		}
	}
	health := registry.Health(testContext(t))
	want := map[string]HealthStatus{
		"healthy":  HealthStatusHealthy,
		"degraded": HealthStatusDegraded,
		"errored":  HealthStatusUnavailable,
		"unknown":  HealthStatusUnavailable,
		"empty":    HealthStatusUnavailable,
	}
	for name, wantStatus := range want {
		if health[name] != wantStatus {
			t.Fatalf("Health()[%q] = %q, want %q", name, health[name], wantStatus)
		}
	}
}

func TestRegistryHealthIsConcurrencySafe(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"wechat", "alipay"} {
		if err := registry.Register(newTestUnconfiguredChannel(t, name, nil)); err != nil {
			t.Fatalf("Register(%s) returned error: %v", name, err)
		}
	}
	ctx := testContext(t)
	var waitGroup sync.WaitGroup
	for index := 0; index < 16; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_ = registry.Names()
			_, _ = registry.Get("wechat")
			_ = registry.Health(ctx)
		}()
	}
	waitGroup.Wait()
}

// stubChannel lets the registry tests exercise status degradation without a real
// provider.
type stubChannel struct {
	name        string
	status      HealthStatus
	healthError error
}

func (stub stubChannel) Name() string { return stub.name }
func (stub stubChannel) CreatePayment(context.Context, CreatePaymentRequest) (CreatePaymentResult, error) {
	return CreatePaymentResult{}, ErrChannelNotConfigured
}
func (stub stubChannel) VerifyNotification(context.Context, NotificationRequest) (NotificationEvent, error) {
	return NotificationEvent{}, ErrSignatureInvalid
}
func (stub stubChannel) Refund(context.Context, RefundRequest) (RefundResult, error) {
	return RefundResult{}, ErrChannelNotConfigured
}
func (stub stubChannel) FetchStatement(context.Context, time.Time) (Statement, error) {
	return Statement{}, ErrChannelNotConfigured
}
func (stub stubChannel) HealthCheck(context.Context) (HealthStatus, error) {
	return stub.status, stub.healthError
}

func TestHTTPHeaderFromFlatCanonicalizesKeys(t *testing.T) {
	// A provider may send the header in any case; map iteration order decides
	// which duplicate wins, so only the canonical key and the single-value rule
	// are asserted here.
	converted := httpHeaderFromFlat(map[string]string{
		" x-signature ": "sig",
		"":              "ignored",
	})
	if got := converted.Get(HMACHeaderName); got != "sig" {
		t.Fatalf("canonical header = %q, want %q", got, "sig")
	}
	if len(converted) != 1 {
		t.Fatalf("converted headers = %v, want a single canonical key", converted)
	}
	if values := converted[HMACHeaderName]; len(values) != 1 {
		t.Fatalf("header values = %v, want exactly one", values)
	}
}

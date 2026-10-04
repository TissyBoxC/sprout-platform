// Package httpx contains bounded HTTP helpers shared by provider adapters.
package httpx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultErrorBodyBytes bounds provider error bodies so a hostile or broken
	// endpoint cannot exhaust gateway memory.
	DefaultErrorBodyBytes int64 = 64 << 10

	// DefaultAudioBodyBytes bounds a complete synthesized audio response.
	DefaultAudioBodyBytes int64 = 8 << 20
)

// ProviderError is a stable error returned for a non-success provider reply.
// It intentionally stores only a bounded diagnostic snippet, never credentials
// or full audio bodies.
type ProviderError struct {
	StatusCode int
	Code       string
	Message    string
	Retryable  bool
}

// Error implements error without including request headers or request bodies.
func (e *ProviderError) Error() string {
	if e == nil {
		return "provider request failed"
	}
	parts := []string{"provider request failed"}
	if e.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	if e.Message != "" {
		parts = append(parts, "message="+e.Message)
	}
	return strings.Join(parts, " ")
}

// JoinURL joins a provider base URL and a relative endpoint path.
func JoinURL(baseURL string, endpointPath string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return "", errors.New("provider base URL is empty")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse provider base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", fmt.Errorf("provider base URL has unsupported scheme %q", parsed.Scheme)
	}
	path := strings.TrimSpace(endpointPath)
	if path == "" {
		return parsed.String(), nil
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") ||
		strings.HasPrefix(path, "ws://") || strings.HasPrefix(path, "wss://") {
		return path, nil
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(path, "/")
	parsed.RawQuery = ""
	return parsed.String(), nil
}

// ReadBounded reads at most limit bytes and returns an error when the response
// exceeds the limit.
func ReadBounded(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, errors.New("response body limit must be positive")
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read provider response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("provider response exceeds %d bytes", limit)
	}
	return data, nil
}

// CloseResponseBody closes a response body and preserves a close error only
// when no earlier error is being returned.
func CloseResponseBody(body io.Closer, current error) error {
	if body == nil {
		return current
	}
	if closeErr := body.Close(); closeErr != nil && current == nil {
		return closeErr
	}
	return current
}

// DoBounded performs one request and returns the response body up to limit.
// The caller's context cancels both the request and the body read.
func DoBounded(
	ctx context.Context,
	client *http.Client,
	request *http.Request,
	limit int64,
) ([]byte, *http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		return nil, nil, err
	}
	body, readErr := ReadBounded(response.Body, limit)
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, response, readErr
	}
	if closeErr != nil {
		return nil, response, closeErr
	}
	return body, response, nil
}

// TencentCloudCredentials contains the signing material for Tencent Cloud
// API 3.0 requests.
type TencentCloudCredentials struct {
	SecretID  string
	SecretKey string
	Service   string
	Region    string
	Host      string
}

// SignTencentCloudRequest signs request in place using the TC3-HMAC-SHA256
// algorithm. The payload bytes must be exactly what will be written to the
// request body.
func SignTencentCloudRequest(
	request *http.Request,
	payload []byte,
	credentials TencentCloudCredentials,
	timestamp time.Time,
) error {
	if request == nil {
		return errors.New("tencent cloud request is nil")
	}
	if strings.TrimSpace(credentials.SecretID) == "" ||
		strings.TrimSpace(credentials.SecretKey) == "" ||
		strings.TrimSpace(credentials.Service) == "" {
		return errors.New("tencent cloud credentials are incomplete")
	}
	host := strings.TrimSpace(credentials.Host)
	if host == "" {
		host = request.Host
	}
	if host == "" && request.URL != nil {
		host = request.URL.Host
	}
	if host == "" {
		return errors.New("tencent cloud request host is empty")
	}

	contentType := strings.TrimSpace(request.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
		request.Header.Set("Content-Type", contentType)
	}
	date := timestamp.UTC().Format("2006-01-02")
	canonicalHeaders := "content-type:" + contentType + "\nhost:" + host + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		request.Method,
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		sha256Hex(payload),
	}, "\n")

	algorithm := "TC3-HMAC-SHA256"
	credentialScope := date + "/" + credentials.Service + "/tc3_request"
	stringToSign := strings.Join([]string{
		algorithm,
		timestamp.UTC().Format(time.RFC3339),
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+credentials.SecretKey), date)
	secretService := hmacSHA256(secretDate, credentials.Service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))
	authorization := fmt.Sprintf(
		"%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		credentials.SecretID,
		credentialScope,
		signedHeaders,
		signature,
	)
	request.Header.Set("Authorization", authorization)
	if credentials.Region != "" {
		request.Header.Set("X-TC-Region", credentials.Region)
	}
	request.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp.UTC().Unix()))
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

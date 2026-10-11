package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/domain"
)

func TestEd25519KeyringVerifierAcceptsValidSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded, err := json.Marshal(map[string]string{
		"ui-text-2026": base64.StdEncoding.EncodeToString(publicKey),
	})
	if err != nil {
		t.Fatalf("marshal keys: %v", err)
	}
	verifier, err := NewEd25519KeyringVerifier(string(encoded))
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	digest := PackageDigest(&domain.PackageDetail{
		Package: domain.Package{
			PackageID:      "home_screen",
			PackageVersion: 1,
			Language:       "zh-CN",
			Platform:       domain.PlatformParentApp,
		},
	})
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(digest)))
	if err := verifier.Verify("ed25519", "ui-text-2026", digest, signature); err != nil {
		t.Fatalf("verify valid signature: %v", err)
	}
}

func TestEd25519KeyringVerifierRejectsUnknownKeyAndTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded, err := json.Marshal(map[string]string{
		"ui-text-2026": base64.StdEncoding.EncodeToString(publicKey),
	})
	if err != nil {
		t.Fatalf("marshal keys: %v", err)
	}
	verifier, err := NewEd25519KeyringVerifier(string(encoded))
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(digest)))
	if err := verifier.Verify(
		"ed25519",
		"unknown-key",
		digest,
		signature,
	); !errors.Is(err, domain.ErrSignatureUnknownKey) {
		t.Fatalf("expected unknown key, got %v", err)
	}
	other := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := verifier.Verify(
		"ed25519",
		"ui-text-2026",
		other,
		signature,
	); !errors.Is(err, domain.ErrSignatureInvalid) {
		t.Fatalf("expected invalid signature, got %v", err)
	}
	if err := verifier.Verify(
		"rsa_pss_sha256",
		"ui-text-2026",
		digest,
		signature,
	); !errors.Is(err, domain.ErrSignatureInvalid) {
		t.Fatalf("expected unsupported algorithm rejection, got %v", err)
	}
}

func TestNewEd25519KeyringVerifierRejectsMalformedKeys(t *testing.T) {
	if _, err := NewEd25519KeyringVerifier(`{"key": "not-base64"}`); err == nil {
		t.Fatal("expected malformed base64 to be rejected")
	}
	short := base64.StdEncoding.EncodeToString([]byte("too-short"))
	if _, err := NewEd25519KeyringVerifier(`{"key": "` + short + `"}`); err == nil {
		t.Fatal("expected short key to be rejected")
	}
	if _, err := NewEd25519KeyringVerifier(`not json`); err == nil {
		t.Fatal("expected non-object JSON to be rejected")
	}
	empty, err := NewEd25519KeyringVerifier("")
	if err != nil {
		t.Fatalf("empty keyring should be allowed: %v", err)
	}
	if err := empty.Verify("ed25519", "key", "digest", "AAAA"); !errors.Is(
		err,
		domain.ErrSignatureUnknownKey,
	) {
		t.Fatalf("empty keyring must fail closed, got %v", err)
	}
}

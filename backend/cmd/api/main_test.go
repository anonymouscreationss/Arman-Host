package main

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := hashPassword("a-long-enough-password")
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$") {
		t.Fatalf("unexpected password format: %q", hash)
	}
	if !verifyPassword(hash, "a-long-enough-password") {
		t.Fatal("verifyPassword() rejected the original password")
	}
	if verifyPassword(hash, "a-different-password") {
		t.Fatal("verifyPassword() accepted a different password")
	}
}

func TestTokenRoundTripAndExpiry(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-access-secret")
	t.Setenv("JWT_REFRESH_SECRET", "test-refresh-secret")
	app := &api{}

	token, err := app.signToken("user-123", "access", time.Minute)
	if err != nil {
		t.Fatalf("signToken() error = %v", err)
	}
	claims, err := app.parseToken(token, "access")
	if err != nil {
		t.Fatalf("parseToken() error = %v", err)
	}
	if claims.UserID != "user-123" || claims.Type != "access" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if _, err := app.parseToken(token, "refresh"); err == nil {
		t.Fatal("parseToken() accepted an access token as refresh")
	}

	expired, err := app.signToken("user-123", "access", -time.Second)
	if err != nil {
		t.Fatalf("signToken() expired error = %v", err)
	}
	if _, err := app.parseToken(expired, "access"); err == nil {
		t.Fatal("parseToken() accepted an expired token")
	}
}

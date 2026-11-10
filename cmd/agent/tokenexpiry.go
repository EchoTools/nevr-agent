package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	tokenExpiryWarnWindow    = 30 * 24 * time.Hour
	tokenExpiryErrorInterval = time.Hour
)

// jwtExpiry decodes (never verifies) a JWT's exp claim. ok is false when the
// token has no exp claim.
func jwtExpiry(token string) (exp time.Time, ok bool, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false, fmt.Errorf("not a JWT: want 3 dot-separated parts, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Exp *float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, false, fmt.Errorf("parse JWT payload: %w", err)
	}
	if claims.Exp == nil {
		return time.Time{}, false, nil
	}
	return time.Unix(int64(*claims.Exp), 0).UTC(), true, nil
}

// checkTokenExpiry logs one line about the token's expiry and reports whether
// it has expired. The token itself is never logged.
func checkTokenExpiry(logger *zap.Logger, token string, now time.Time) (expired bool) {
	exp, ok, err := jwtExpiry(token)
	switch {
	case err != nil:
		logger.Warn("Cannot read ingest JWT expiry", zap.Error(err))
	case !ok:
		logger.Warn("Ingest JWT has no exp claim; its expiry is unknown")
	case !exp.After(now):
		logger.Error("Ingest JWT has expired; reconnects will fail until it is replaced",
			zap.Time("expired_at", exp))
		return true
	case exp.Sub(now) < tokenExpiryWarnWindow:
		logger.Warn("Ingest JWT expires soon",
			zap.Time("expires_at", exp), zap.Duration("remaining", exp.Sub(now).Round(time.Minute)))
	default:
		logger.Info("Ingest JWT expiry", zap.Time("expires_at", exp))
	}
	return false
}

// watchTokenExpiry checks the token at start and, if it is expired, logs an
// ERROR every interval until ctx ends. A live token is not re-logged.
func watchTokenExpiry(ctx context.Context, logger *zap.Logger, token string, interval time.Duration) {
	if !checkTokenExpiry(logger, token, time.Now()) {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkTokenExpiry(logger, token, time.Now())
		}
	}
}

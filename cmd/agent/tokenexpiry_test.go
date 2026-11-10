package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func makeJWT(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc([]byte(claims)) + ".c2lnbmF0dXJl"
}

func jwtExp(t time.Time) string { return makeJWT(fmt.Sprintf(`{"exp":%d}`, t.Unix())) }

func TestCheckTokenExpiry(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		token   string
		level   zapcore.Level
		expired bool
		contain string
	}{
		{"more than 30 days", jwtExp(now.Add(31 * 24 * time.Hour)), zapcore.InfoLevel, false, "2026-12-11"},
		{"within 30 days", jwtExp(now.Add(10 * 24 * time.Hour)), zapcore.WarnLevel, false, "2026-11-20"},
		{"expired", jwtExp(now.Add(-time.Hour)), zapcore.ErrorLevel, true, "expired"},
		{"no exp claim", makeJWT(`{"sub":"x"}`), zapcore.WarnLevel, false, "exp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			expired := checkTokenExpiry(zap.New(core), tt.token, now)
			if expired != tt.expired {
				t.Errorf("expired = %v, want %v", expired, tt.expired)
			}
			entries := logs.All()
			if len(entries) != 1 {
				t.Fatalf("got %d log entries, want 1: %v", len(entries), entries)
			}
			e := entries[0]
			if e.Level != tt.level {
				t.Errorf("level = %v, want %v", e.Level, tt.level)
			}
			if !strings.Contains(e.Message+fmt.Sprint(e.ContextMap()), tt.contain) {
				t.Errorf("entry %q %v does not mention %q", e.Message, e.ContextMap(), tt.contain)
			}
			if strings.Contains(e.Message+fmt.Sprint(e.ContextMap()), tt.token) {
				t.Error("log entry contains the token")
			}
		})
	}
}

func TestCheckTokenExpiry_Malformed(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	checkTokenExpiry(zap.New(core), "not-a-jwt", time.Now())
	if es := logs.All(); len(es) != 1 || es[0].Level != zapcore.WarnLevel {
		t.Fatalf("want one warning for an undecodable token, got %v", es)
	}
}

func TestWatchTokenExpiry_ErrorsEveryIntervalOnceExpired(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		watchTokenExpiry(ctx, zap.New(core), jwtExp(time.Now().Add(-time.Minute)), 20*time.Millisecond)
		close(done)
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
	errs := 0
	for _, e := range logs.All() {
		if e.Level == zapcore.ErrorLevel {
			errs++
		}
	}
	if errs < 3 {
		t.Errorf("got %d error entries, want at least 3 (start + repeats)", errs)
	}
}

// A token valid at start that expires mid-run -- the recorder's case -- must
// start erroring once it passes exp, and not before.
func TestWatchTokenExpiry_ErrorsAfterMidRunExpiry(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	exp := time.Now().Add(2 * time.Second) // exp is whole seconds: expires 1-2 s from now
	go func() {
		watchTokenExpiry(ctx, zap.New(core), jwtExp(exp), 20*time.Millisecond)
		close(done)
	}()
	time.Sleep(500 * time.Millisecond)
	for _, e := range logs.All() {
		if e.Level == zapcore.ErrorLevel {
			t.Fatal("error logged before the token expired")
		}
	}
	time.Sleep(2200 * time.Millisecond)
	cancel()
	<-done
	errs := 0
	for _, e := range logs.All() {
		if e.Level == zapcore.ErrorLevel {
			errs++
		}
	}
	if errs < 2 {
		t.Errorf("got %d error entries after mid-run expiry, want at least 2", errs)
	}
}

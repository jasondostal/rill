package db

// Internal-package tests (package db, not db_test) — exercise unexported
// helpers like isConnClosedErr that are part of the reconnect supervisor's
// brain. Keeping these out of the external surrealdb_test.go.

import (
	"errors"
	"testing"
)

func TestIsConnClosedErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection closed", errors.New("rpc: connection closed"), true},
		{"websocket close frame", errors.New("websocket: close 1006 (abnormal closure)"), true},
		{"broken pipe", errors.New("write tcp: broken pipe"), true},
		{"use of closed network connection", errors.New("use of closed network connection"), true},
		{"context deadline (not retryable)", errors.New("context deadline exceeded"), false},
		{"query syntax error (not retryable)", errors.New("parse error at line 3"), false},
		{"auth failure (not retryable)", errors.New("signin failed: unauthorized"), false},
		{"plain EOF (ambiguous, excluded)", errors.New("EOF"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConnClosedErr(tc.err); got != tc.want {
				t.Errorf("isConnClosedErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

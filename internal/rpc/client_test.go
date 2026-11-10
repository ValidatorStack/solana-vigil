package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallDecodesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": "ok"})
	}))
	defer server.Close()
	client := New(server.URL, Options{})
	var result string
	if err := client.Call(context.Background(), "getHealth", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("got %q", result)
	}
}

func TestCallRetriesRateLimitAndHonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":42}`))
	}))
	defer server.Close()
	client := New(server.URL, Options{MaxRetries: 1})
	var result int
	start := time.Now()
	if err := client.Call(context.Background(), "getSlot", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result != 42 || calls.Load() != 2 {
		t.Fatalf("result=%d calls=%d", result, calls.Load())
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Fatal("Retry-After was not honored")
	}
}

func TestCallDoesNotRetryRPCError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad params"}}`))
	}))
	defer server.Close()
	client := New(server.URL, Options{MaxRetries: 3})
	err := client.Call(context.Background(), "getSlot", nil, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32602 {
		t.Fatalf("expected RPC error, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("RPC error retried %d times", calls.Load())
	}
}

func TestOptionalStringDistinguishesAbsentNullAndValue(t *testing.T) {
	var absent VoteAccount
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.BLSPubkey.Present {
		t.Fatal("absent field marked present")
	}
	var nullValue VoteAccount
	if err := json.Unmarshal([]byte(`{"blsPubkeyCompressed":null}`), &nullValue); err != nil {
		t.Fatal(err)
	}
	if !nullValue.BLSPubkey.Present || !nullValue.BLSPubkey.Null {
		t.Fatalf("null not preserved: %+v", nullValue.BLSPubkey)
	}
	var value VoteAccount
	if err := json.Unmarshal([]byte(`{"blsPubkeyCompressed":"key"}`), &value); err != nil {
		t.Fatal(err)
	}
	if value.BLSPubkey.Value != "key" || value.BLSPubkey.Null {
		t.Fatalf("value not decoded: %+v", value.BLSPubkey)
	}
}

func TestVoteAccountBLSJSONRoundTripPreservesPresence(t *testing.T) {
	for _, test := range []struct {
		name, input, expected string
		absent                bool
	}{
		{name: "absent", input: `{}`, absent: true},
		{name: "null", input: `{"blsPubkeyCompressed":null}`, expected: "null"},
		{name: "value", input: `{"blsPubkeyCompressed":"key"}`, expected: `"key"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var account VoteAccount
			if err := json.Unmarshal([]byte(test.input), &account); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(account)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			actual, present := fields["blsPubkeyCompressed"]
			if test.absent && present {
				t.Fatalf("absent field was exported: %s", encoded)
			}
			if !test.absent && (!present || string(actual) != test.expected) {
				t.Fatalf("round trip got %s, want %s", actual, test.expected)
			}
		})
	}
}

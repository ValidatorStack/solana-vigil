// Package rpc implements a bounded, retrying Solana JSON-RPC client.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/buildinfo"
)

const maxResponseBytes = 32 << 20

type Client struct {
	endpoint string
	http     *http.Client
	timeout  time.Duration
	retries  int
	id       atomic.Uint64
	limit    chan struct{}
	logger   *slog.Logger
}

type Options struct {
	Timeout     time.Duration
	MaxRetries  int
	Concurrency int
	HTTPClient  *http.Client
	Logger      *slog.Logger
}

func New(endpoint string, options Options) *Client {
	if options.Timeout <= 0 {
		options.Timeout = 8 * time.Second
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 4
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{}
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Client{
		endpoint: endpoint,
		http:     options.HTTPClient,
		timeout:  options.Timeout,
		retries:  options.MaxRetries,
		limit:    make(chan struct{}, options.Concurrency),
		logger:   options.Logger,
	}
}

type RPCError struct {
	Method  string
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s: RPC error %d: %s", e.Method, e.Code, e.Message)
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data,omitempty"`
	} `json:"error,omitempty"`
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	select {
	case c.limit <- struct{}{}:
		defer func() { <-c.limit }()
	case <-ctx.Done():
		return ctx.Err()
	}

	payload, err := json.Marshal(request{JSONRPC: "2.0", ID: c.id.Add(1), Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, lastErr)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		lastErr = c.callOnce(ctx, method, payload, result)
		if lastErr == nil {
			return nil
		}
		if !retryable(lastErr) || ctx.Err() != nil {
			return lastErr
		}
		c.logger.Warn("transient RPC request failure", "method", method, "attempt", attempt+1, "error", lastErr)
	}
	return lastErr
}

func (c *Client) callOnce(ctx context.Context, method string, payload []byte, result any) error {
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s request failed: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		retryAfter, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
		return &HTTPError{Method: method, Status: resp.StatusCode, RetryAfter: retryAfter}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return &HTTPError{Method: method, Status: resp.StatusCode}
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes+1))
	var envelope response
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if envelope.Error != nil {
		return &RPCError{Method: method, Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	if len(envelope.Result) == 0 {
		return fmt.Errorf("%s: response has neither result nor error", method)
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

type HTTPError struct {
	Method     string
	Status     int
	RetryAfter int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s: HTTP status %d", e.Method, e.Status)
}

func retryable(err error) bool {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == 429 || httpErr.Status >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func retryDelay(attempt int, err error) time.Duration {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.RetryAfter > 0 {
		seconds := httpErr.RetryAfter
		if seconds > 10 {
			seconds = 10
		}
		return time.Duration(seconds) * time.Second
	}
	base := time.Duration(100*(1<<min(attempt-1, 5))) * time.Millisecond
	jitter := time.Duration(rand.Int63n(int64(base/2 + 1)))
	return base + jitter
}

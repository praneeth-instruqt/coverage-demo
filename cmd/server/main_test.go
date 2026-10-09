package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func testEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRunServesAndShutsDown(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, discard, testEnv(map[string]string{
			"ADDR":            addr,
			"JWT_SECRET":      "0123456789abcdef0123456789abcdef",
			"HASH_ITERATIONS": "1000",
		}))
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("healthz = %d", res.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancel")
	}
}

func TestRunConfigError(t *testing.T) {
	if err := run(context.Background(), discard, testEnv(nil)); err == nil {
		t.Error("expected error for missing JWT_SECRET")
	}
}

func TestRunListenError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	err = run(context.Background(), discard, testEnv(map[string]string{
		"ADDR":            l.Addr().String(), // already in use
		"JWT_SECRET":      "0123456789abcdef0123456789abcdef",
		"HASH_ITERATIONS": "1000",
	}))
	if err == nil {
		t.Error("expected listen error for occupied port")
	}
}

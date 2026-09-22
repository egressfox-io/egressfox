package healthcheck_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/egressfox-io/egressfox/internal/healthcheck"
)

func TestSOCKSAuthenticationNegotiation(t *testing.T) {
	t.Parallel()
	address := serveAuthentication(t, "egressfox", "synthetic-password")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := healthcheck.CheckSOCKSAuthentication(ctx, address, "egressfox", "synthetic-password"); err != nil {
		t.Fatalf("valid authentication failed: %v", err)
	}
}

func TestSOCKSAuthenticationRejectsWrongCredentials(t *testing.T) {
	t.Parallel()
	address := serveAuthentication(t, "egressfox", "synthetic-password")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := healthcheck.CheckSOCKSAuthentication(ctx, address, "egressfox", "wrong-password"); !errors.Is(err, healthcheck.ErrNotReady) {
		t.Fatalf("wrong authentication error = %v", err)
	}
}

func serveAuthentication(t *testing.T, username, password string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		greeting := make([]byte, 3)
		if _, err := io.ReadFull(connection, greeting); err != nil || greeting[0] != 5 || greeting[2] != 2 {
			return
		}
		_, _ = connection.Write([]byte{5, 2})
		header := make([]byte, 2)
		if _, err := io.ReadFull(connection, header); err != nil || header[0] != 1 {
			return
		}
		user := make([]byte, int(header[1]))
		if _, err := io.ReadFull(connection, user); err != nil {
			return
		}
		if _, err := io.ReadFull(connection, header[:1]); err != nil {
			return
		}
		pass := make([]byte, int(header[0]))
		if _, err := io.ReadFull(connection, pass); err != nil {
			return
		}
		status := byte(1)
		if string(user) == username && string(pass) == password {
			status = 0
		}
		_, _ = connection.Write([]byte{1, status})
	}()
	return listener.Addr().String()
}

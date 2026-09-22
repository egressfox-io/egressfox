// Package healthcheck implements the narrow authenticated SOCKS readiness
// negotiation used by M7 managed engine Pods. It does not proxy traffic.
package healthcheck

import (
	"context"
	"errors"
	"io"
	"net"
	"time"
)

var ErrNotReady = errors.New("authenticated SOCKS listener is not ready")

func CheckSOCKSAuthentication(ctx context.Context, address, username, password string) error {
	if address == "" || len(username) < 1 || len(username) > 255 || len(password) < 1 || len(password) > 255 {
		return ErrNotReady
	}
	connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return ErrNotReady
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := connection.Write([]byte{5, 1, 2}); err != nil {
		return ErrNotReady
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(connection, response); err != nil || response[0] != 5 || response[1] != 2 {
		return ErrNotReady
	}
	request := make([]byte, 0, 3+len(username)+len(password))
	request = append(request, 1, byte(len(username)))
	request = append(request, username...)
	request = append(request, byte(len(password)))
	request = append(request, password...)
	if _, err := connection.Write(request); err != nil {
		return ErrNotReady
	}
	if _, err := io.ReadFull(connection, response); err != nil || response[0] != 1 || response[1] != 0 {
		return ErrNotReady
	}
	return nil
}

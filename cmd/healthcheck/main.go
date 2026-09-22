package main

import (
	"context"
	"flag"
	"os"
	"strings"
	"time"

	"github.com/egressfox-io/egressfox/internal/healthcheck"
)

func main() {
	var address, usernameFile, passwordFile string
	flag.StringVar(&address, "address", "127.0.0.1:1080", "loopback SOCKS listener address")
	flag.StringVar(&usernameFile, "username-file", "", "mounted username file")
	flag.StringVar(&passwordFile, "password-file", "", "mounted password file")
	flag.Parse()
	username, usernameOK := readCredential(usernameFile)
	password, passwordOK := readCredential(passwordFile)
	if !usernameOK || !passwordOK {
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if healthcheck.CheckSOCKSAuthentication(ctx, address, username, password) != nil {
		os.Exit(1)
	}
}

func readCredential(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	value, err := os.ReadFile(path)
	if err != nil || len(value) == 0 || len(value) > 256 {
		return "", false
	}
	return strings.TrimSpace(string(value)), true
}

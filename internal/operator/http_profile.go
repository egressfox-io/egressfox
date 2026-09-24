package operator

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
)

// profileHeaders resolves the effective static request context. An empty
// User-Agent value is deliberate: net/http then suppresses its implicit agent.
func profileHeaders(clientIdentity string, profile *egressv1alpha1.HTTPClientProfile) (http.Header, error) {
	headers := make(http.Header)
	mode := "Default"
	if profile != nil && profile.Mode != "" {
		mode = profile.Mode
	}
	switch mode {
	case "Default":
		headers.Set("User-Agent", "Happ/1.0")
		headers.Set("X-Hwid", stableHWID(clientIdentity))
		headers.Set("X-Device-Os", "iOS")
		headers.Set("X-Ver-Os", "18.3")
		headers.Set("X-Device-Model", "iPhone 14 Pro Max")
	case "Clean":
		headers[http.CanonicalHeaderKey("User-Agent")] = []string{""}
		if profile != nil && (profile.UserAgent != "" || profile.HWID != "" || profile.DeviceOS != "" || profile.OSVersion != "" || profile.DeviceModel != "" || len(profile.Headers) != 0) {
			return nil, poolFailure("profile_clean_conflict")
		}
	default:
		return nil, poolFailure("profile_mode")
	}
	if profile == nil {
		return headers, nil
	}
	for _, entry := range []struct{ key, value string }{
		{"User-Agent", profile.UserAgent}, {"X-Hwid", profile.HWID},
		{"X-Device-Os", profile.DeviceOS}, {"X-Ver-Os", profile.OSVersion},
		{"X-Device-Model", profile.DeviceModel},
	} {
		if entry.value == "" {
			continue
		}
		if !validHeaderValue(entry.value, 256) {
			return nil, poolFailure("profile_header_value")
		}
		headers.Set(entry.key, entry.value)
	}
	if len(profile.Headers) > 16 {
		return nil, poolFailure("profile_header_limit")
	}
	for key, value := range profile.Headers {
		if !validHeaderName(key) || !validHeaderValue(value, 1024) {
			return nil, poolFailure("profile_header_invalid")
		}
		canonical := http.CanonicalHeaderKey(key)
		if reservedTransportOrProfileHeader(canonical) || likelyCredentialHeader(canonical) {
			return nil, poolFailure("profile_header_reserved")
		}
		if _, exists := headers[canonical]; exists {
			return nil, poolFailure("profile_header_duplicate")
		}
		headers.Set(canonical, value)
	}
	return headers, nil
}

func reservedTransportOrProfileHeader(name string) bool {
	switch strings.ToLower(name) {
	case "user-agent", "x-hwid", "x-device-os", "x-ver-os", "x-device-model",
		"host", "connection", "content-length", "transfer-encoding", "proxy-authorization", "set-cookie",
		"accept-encoding", "if-none-match", "if-modified-since", "referer", "upgrade", "te", "trailer":
		return true
	default:
		return false
	}
}

func likelyCredentialHeader(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || strings.Contains(lower, "token") || strings.Contains(lower, "api-key") || strings.Contains(lower, "secret") || strings.Contains(lower, "password")
}

func stableHWID(identity string) string {
	material := "egressfox-http-client-hwid-v2\x00" + identity
	if strings.HasPrefix(identity, "legacy:") {
		parts := strings.SplitN(strings.TrimPrefix(identity, "legacy:"), "/", 2)
		material = "egressfox-http-client-hwid-v1\x00" + parts[0] + "\x00" + parts[1]
	}
	digest := sha256.Sum256([]byte(material))
	digest[6] = digest[6]&0x0f | 0x80
	digest[8] = digest[8]&0x3f | 0x80
	value := hex.EncodeToString(digest[:16])
	return strings.ToUpper(value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:32])
}

func clientIdentity(poolUID, sourceID, explicit string) (string, error) {
	if explicit == "" {
		return "legacy:" + poolUID + "/" + sourceID, nil
	}
	if len(explicit) > 160 {
		return "", poolFailure("client_identity")
	}
	prefix, value, found := strings.Cut(explicit, ":")
	if !found {
		return "", poolFailure("client_identity")
	}
	if prefix == "legacy" {
		uid, previousID, found := strings.Cut(value, "/")
		if !found || !validIdentityPart(uid, 80) || !validIdentityPart(previousID, 63) {
			return "", poolFailure("client_identity")
		}
		return explicit, nil
	}
	if prefix != "stable" || !validIdentityPart(value, 128) {
		return "", poolFailure("client_identity")
	}
	return explicit, nil
}

func validIdentityPart(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

func validHeaderName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", ch) {
			continue
		}
		return false
	}
	return true
}

func validHeaderValue(value string, max int) bool {
	if len(value) > max {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

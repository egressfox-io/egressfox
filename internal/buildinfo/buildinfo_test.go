package buildinfo

import (
	"strings"
	"testing"
)

func TestCurrentIsBoundedAndNamesExactProfiles(t *testing.T) {
	value := Current().String()
	for _, expected := range []string{"egressfox-operator", "mihomo/1.19.31", "sing-box/1.14.1", "go="} {
		if !strings.Contains(value, expected) {
			t.Fatalf("version output %q does not contain %q", value, expected)
		}
	}
	if len(value) > 512 || strings.ContainsAny(value, "\r\n") {
		t.Fatalf("version output is unbounded: %q", value)
	}
}

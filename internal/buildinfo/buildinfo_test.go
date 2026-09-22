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

func TestEffectiveVersionMarksDirtyBuilds(t *testing.T) {
	clean := Identity{Version: "0.1.0-dev.1+g0123456789ab"}
	if clean.EffectiveVersion() != "0.1.0-dev.1+g0123456789ab" {
		t.Fatalf("clean build identity = %q", clean.EffectiveVersion())
	}
	dirty := clean
	dirty.Dirty = true
	if want := "0.1.0-dev.1+g0123456789ab" + DirtySuffix; dirty.EffectiveVersion() != want {
		t.Fatalf("dirty build identity = %q, want %q", dirty.EffectiveVersion(), want)
	}
	if dirty.EffectiveVersion() == clean.EffectiveVersion() {
		t.Fatal("dirty build must not claim the identity of a clean build")
	}
}

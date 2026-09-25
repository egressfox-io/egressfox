package observation

import (
	"testing"
	"time"
)

func TestTargetRevisionSeparatesAuthorizationAndProbeSemantics(t *testing.T) {
	id, err := NewTargetID("pool-profile")
	if err != nil {
		t.Fatal(err)
	}
	makeTarget := func(url string, status int, salt string) HTTPTarget {
		target, err := NewHTTPTarget(id, url, status, time.Second, HTTPOptions{RevisionSalt: salt})
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	base := makeTarget("https://example.com/a", 204, "")
	for _, changed := range []HTTPTarget{
		makeTarget("https://example.com/b", 204, ""),
		makeTarget("https://example.com/a", 200, ""),
		makeTarget("https://example.com/a", 204, "private-targets-allowed"),
	} {
		if base.Ref().Equal(changed.Ref()) {
			t.Fatal("incompatible target revision reused")
		}
	}
}

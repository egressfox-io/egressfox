package controller

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

func TestRequeueAfterIsStableAndBounded(t *testing.T) {
	base := 5 * time.Minute
	uid := types.UID("11111111-2222-3333-4444-555555555555")
	first := requeueAfter(base, uid)
	second := requeueAfter(base, uid)
	if first != second || first < base || first > base+base/10 {
		t.Fatalf("requeue interval = %s then %s, want stable value in [%s,%s]", first, second, base, base+base/10)
	}
	if fallback := requeueAfter(0, uid); fallback < base || fallback > base+base/10 {
		t.Fatalf("fallback requeue interval = %s", fallback)
	}
}

package controller

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
)

func maxRequeue(base time.Duration) time.Duration {
	return base + base*operatoradapter.RequeueJitterPermille/1000
}

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
	// The documented jitter bound holds for every object, not just one UID.
	for i := range 500 {
		uid := types.UID(fmt.Sprintf("uid-%d", i))
		if got := requeueAfter(14*time.Minute, uid); got < 14*time.Minute || got > maxRequeue(14*time.Minute) {
			t.Fatalf("requeue %s outside the jitter bound", got)
		}
	}
}

// A long or jittered source refresh must not stretch evidence maintenance:
// Gateways requeue at the evidence cadence, pools keep their refresh cadence.
func TestGatewayRequeueFollowsEvidenceCadence(t *testing.T) {
	cadence := operatoradapter.EvidenceCadence()
	for _, refresh := range []time.Duration{14 * time.Minute, time.Hour, 24 * time.Hour} {
		for i := range 200 {
			uid := types.UID(fmt.Sprintf("gateway-%d", i))
			if got := gatewayRequeue(refresh, uid); got < cadence || got > maxRequeue(cadence) {
				t.Fatalf("refresh=%s gateway requeue=%s want within [%s,%s]", refresh, got, cadence, maxRequeue(cadence))
			}
			if pool := requeueAfter(refresh, uid); pool < refresh {
				t.Fatalf("refresh=%s pool requeue=%s shortened source acquisition", refresh, pool)
			}
		}
	}
	// A refresh shorter than the cadence keeps reconciling at the refresh rate.
	short := time.Minute
	if cadence > short {
		if got := gatewayRequeue(short, "gateway"); got > maxRequeue(short) {
			t.Fatalf("short refresh requeue=%s", got)
		}
	}
}

package controller

import (
	"context"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	egressv1alpha1 "github.com/egressfox-io/egressfox/api/v1alpha1"
	operatoradapter "github.com/egressfox-io/egressfox/internal/operator"
	"github.com/egressfox-io/egressfox/internal/state"
)

const maxHTTPRefreshWorkers = 4

type refreshSlot struct {
	fingerprint [32]byte
	next        time.Time
	active      bool
	failure     string
	lastChange  time.Time
	pool        *egressv1alpha1.ProxyPool
	source      egressv1alpha1.SubscriptionSource
}

// HTTPRefresher is a leader-scoped runnable with a fixed worker budget. It only
// stores scheduling state in memory; the accepted revision lives in SQLite.
type HTTPRefresher struct {
	Reader        client.Reader
	Store         *state.Store
	Namespace     string
	Events        chan event.GenericEvent
	GatewayEvents chan event.GenericEvent
	notify        chan types.NamespacedName
	mu            sync.Mutex
	slots         map[string]*refreshSlot
	Now           func() time.Time
}

func NewHTTPRefresher(reader client.Reader, store *state.Store, namespace string) *HTTPRefresher {
	return &HTTPRefresher{Reader: reader, Store: store, Namespace: namespace, Events: make(chan event.GenericEvent, 128), GatewayEvents: make(chan event.GenericEvent, 128), notify: make(chan types.NamespacedName, 128), slots: map[string]*refreshSlot{}, Now: time.Now}
}

func (*HTTPRefresher) NeedLeaderElection() bool { return true }

func (r *HTTPRefresher) Notify(pool *egressv1alpha1.ProxyPool) {
	if r == nil || pool == nil {
		return
	}
	select {
	case r.notify <- types.NamespacedName{Namespace: pool.Namespace, Name: pool.Name}:
	default:
	}
}

func (r *HTTPRefresher) Start(ctx context.Context) error {
	dispatchTicker := time.NewTicker(5 * time.Second)
	scanTicker := time.NewTicker(5 * time.Minute)
	defer dispatchTicker.Stop()
	defer scanTicker.Stop()
	work := make(chan refreshJob, maxHTTPRefreshWorkers)
	var workers sync.WaitGroup
	for range maxHTTPRefreshWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range work {
				r.runJob(ctx, job)
			}
		}()
	}
	defer func() { close(work); workers.Wait() }()
	r.scan(ctx)
	r.dispatch(work)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-dispatchTicker.C:
			r.dispatch(work)
		case <-scanTicker.C:
			r.scan(ctx)
			r.dispatch(work)
		case name := <-r.notify:
			r.inspect(ctx, name)
			r.dispatch(work)
		}
	}
}

type refreshJob struct {
	pool        *egressv1alpha1.ProxyPool
	source      egressv1alpha1.SubscriptionSource
	key         string
	fingerprint [32]byte
}

func (r *HTTPRefresher) scan(ctx context.Context) {
	list := &egressv1alpha1.ProxyPoolList{}
	if err := r.Reader.List(ctx, list, client.InNamespace(r.Namespace)); err != nil {
		return
	}
	live := map[string]struct{}{}
	for i := range list.Items {
		pool := &list.Items[i]
		for _, src := range pool.Spec.Sources {
			if src.HTTP != nil {
				live[string(pool.UID)+"/"+src.ID] = struct{}{}
			}
		}
		r.enqueue(ctx, pool)
	}
	r.mu.Lock()
	for key := range r.slots {
		if _, ok := live[key]; !ok {
			delete(r.slots, key)
		}
	}
	r.mu.Unlock()
	_ = r.Store.PruneSourceCache(ctx, live, r.Now())
}

func (r *HTTPRefresher) inspect(ctx context.Context, name types.NamespacedName) {
	pool := &egressv1alpha1.ProxyPool{}
	if err := r.Reader.Get(ctx, name, pool); err != nil {
		if !apierrors.IsNotFound(err) {
			return
		}
		r.mu.Lock()
		for key, slot := range r.slots {
			if slot.pool != nil && slot.pool.Namespace == name.Namespace && slot.pool.Name == name.Name {
				delete(r.slots, key)
				_ = r.Store.DeleteSourceCache(ctx, key)
			}
		}
		r.mu.Unlock()
		return
	}
	r.enqueue(ctx, pool)
	live := map[string]struct{}{}
	for _, desired := range pool.Spec.Sources {
		if desired.HTTP != nil {
			live[string(pool.UID)+"/"+desired.ID] = struct{}{}
		}
	}
	r.mu.Lock()
	for key := range r.slots {
		if strings.HasPrefix(key, string(pool.UID)+"/") {
			if _, ok := live[key]; !ok {
				delete(r.slots, key)
				_ = r.Store.DeleteSourceCache(ctx, key)
			}
		}
	}
	r.mu.Unlock()
}

func (r *HTTPRefresher) enqueue(ctx context.Context, pool *egressv1alpha1.ProxyPool) {
	now := r.Now()
	for _, desired := range pool.Spec.Sources {
		if desired.HTTP == nil {
			continue
		}
		config, err := operatoradapter.ResolveHTTP(ctx, r.Reader, pool, desired)
		if err != nil {
			continue
		}
		key := config.Key
		r.mu.Lock()
		slot, ok := r.slots[key]
		if !ok {
			interval := refreshInterval(durationValue(pool.Spec.RefreshInterval))
			slot = &refreshSlot{fingerprint: config.Fingerprint, next: now.Add(interval + spread(key, interval/10))}
			r.slots[key] = slot
			// Missing cache should refresh promptly; restart jitter still spreads it.
			if entry, found, _ := r.Store.LoadSourceCache(ctx, key, config.Fingerprint); found {
				due := entry.ValidatedAt.Add(interval + spread(key, interval/10))
				if !due.After(now) {
					slot.next = now.Add(spread(key, 10*time.Second))
				} else if due.Before(slot.next) {
					slot.next = due
				}
			} else {
				_ = r.Store.DeleteSourceCache(ctx, key)
				slot.next = now.Add(spread(key, 10*time.Second))
			}
		} else if slot.fingerprint != config.Fingerprint {
			_ = r.Store.DeleteSourceCache(ctx, key)
			slot.fingerprint = config.Fingerprint
			slot.next = now
			slot.failure = ""
		}
		slot.pool, slot.source = pool.DeepCopy(), desired
		r.mu.Unlock()
	}
}

func (r *HTTPRefresher) dispatch(work chan<- refreshJob) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.Now()
	keys := make([]string, 0, len(r.slots))
	for key := range r.slots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		slot := r.slots[key]
		if slot.active || slot.pool == nil || now.Before(slot.next) {
			continue
		}
		select {
		case work <- refreshJob{pool: slot.pool.DeepCopy(), source: slot.source, key: key, fingerprint: slot.fingerprint}:
			slot.active = true
			interval := refreshInterval(durationValue(slot.pool.Spec.RefreshInterval))
			slot.next = now.Add(interval + spread(key, interval/10))
		default:
			return
		}
	}
}

func spread(key string, window time.Duration) time.Duration {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return time.Duration(uint64(window) * uint64(h.Sum32()%1000) / 1000)
}

func (r *HTTPRefresher) runJob(ctx context.Context, job refreshJob) {
	defer func() {
		r.mu.Lock()
		if slot := r.slots[job.key]; slot != nil {
			slot.active = false
			if slot.fingerprint == job.fingerprint && slot.pool != nil {
				interval := refreshInterval(durationValue(slot.pool.Spec.RefreshInterval))
				next := r.Now().Add(interval + spread(job.key, interval/10))
				if next.After(slot.next) {
					slot.next = next
				}
			}
		}
		r.mu.Unlock()
	}()
	if ctx.Err() != nil {
		return
	}
	changed, err := operatoradapter.RefreshHTTP(ctx, r.Reader, r.Store, job.pool, job.source, r.Now().UTC())
	if ctx.Err() != nil {
		return
	}
	if coded, ok := err.(interface{ Code() string }); ok && coded.Code() == "source_obsolete" {
		return
	}
	r.mu.Lock()
	slot := r.slots[job.key]
	previousFailure := ""
	if slot != nil && slot.fingerprint == job.fingerprint {
		previousFailure = slot.failure
		if err != nil {
			slot.failure = "RefreshFailed"
		} else {
			slot.failure = ""
			if changed {
				slot.lastChange = r.Now().UTC().Truncate(time.Second)
			}
		}
	}
	r.mu.Unlock()
	if changed || err != nil || previousFailure != "" {
		select {
		case r.Events <- event.GenericEvent{Object: job.pool}:
		case <-ctx.Done():
		}
	}
	if changed {
		select {
		case r.GatewayEvents <- event.GenericEvent{Object: job.pool}:
		case <-ctx.Done():
		}
	}
}

func (r *HTTPRefresher) LastChange(poolUID types.UID) time.Time {
	if r == nil {
		return time.Time{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest time.Time
	for key, slot := range r.slots {
		if strings.HasPrefix(key, string(poolUID)+"/") && slot.lastChange.After(latest) {
			latest = slot.lastChange
		}
	}
	return latest
}

func (r *HTTPRefresher) Failure(key string) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if slot := r.slots[key]; slot != nil {
		return slot.failure
	}
	return ""
}

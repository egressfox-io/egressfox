package source

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// RetryFetch bounds transient HTTP failures. The caller supplies delay so tests
// can be deterministic; each attempt repeats destination authorization in Fetch.
func RetryFetch(ctx context.Context, fetch func(context.Context) (FetchResult, error), delay func(int) time.Duration) (FetchResult, error) {
	var result FetchResult
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		result, err = fetch(ctx)
		if err == nil || !Retryable(err) || attempt == 3 || ctx.Err() != nil {
			return result, err
		}
		wait := time.Duration(1<<(attempt-1))*200*time.Millisecond + time.Duration(rand.Int64N(int64(100*time.Millisecond)))
		if delay != nil {
			wait = delay(attempt)
		}
		var failure *Failure
		if errors.As(err, &failure) && failure.retryAfter > wait {
			wait = failure.retryAfter
		}
		if wait < 0 {
			wait = 0
		}
		if wait > 2*time.Second {
			wait = 2 * time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return FetchResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	return result, err
}

func Retryable(err error) bool {
	var failure *Failure
	if !errors.As(err, &failure) {
		return false
	}
	if failure.code == "timeout" || failure.code == "request" || failure.code == "resolve" {
		return true
	}
	return failure.code == "http_status" && (failure.status == 429 || failure.status >= 500 && failure.status <= 599)
}

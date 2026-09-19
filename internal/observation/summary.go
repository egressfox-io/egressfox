package observation

import (
	"bytes"
	"errors"
	"sort"
	"time"
)

type Summary struct {
	Key                  Key
	Samples              int
	Successes            int
	SuccessRatio         float64
	Latest               *Observation
	LatestSuccess        *Observation
	MeanSuccessDuration  time.Duration
	ConsecutiveSuccesses int
	ConsecutiveFailures  int
	Fresh                bool
}

func Summarize(key Key, values []Observation, now time.Time, window, freshness time.Duration) (Summary, error) {
	if _, err := NewKey(key.connection, key.target, key.vantage, key.kind, key.profile); err != nil {
		return Summary{}, err
	}
	if now.IsZero() || window <= 0 || freshness <= 0 {
		return Summary{}, errors.New("summary requires explicit positive time bounds")
	}
	now = now.UTC()
	cutoff := now.Add(-window)
	filtered := make([]Observation, 0, len(values))
	for _, value := range values {
		if !sameKey(key, value.key) {
			return Summary{}, errors.New("summary input contains a different evidence key")
		}
		if value.completedAt.Before(cutoff) || value.completedAt.After(now) {
			continue
		}
		filtered = append(filtered, value)
	}
	sort.Slice(filtered, func(left, right int) bool {
		if !filtered[left].completedAt.Equal(filtered[right].completedAt) {
			return filtered[left].completedAt.Before(filtered[right].completedAt)
		}
		return bytes.Compare(filtered[left].sampleID[:], filtered[right].sampleID[:]) < 0
	})
	result := Summary{Key: key, Samples: len(filtered)}
	var total time.Duration
	for index := range filtered {
		value := filtered[index]
		if value.Successful() {
			result.Successes++
			total += value.duration
			copy := value
			result.LatestSuccess = &copy
		}
	}
	if result.Samples > 0 {
		latest := filtered[len(filtered)-1]
		result.Latest = &latest
		result.SuccessRatio = float64(result.Successes) / float64(result.Samples)
		result.Fresh = now.Sub(latest.completedAt) <= freshness
		for index := len(filtered) - 1; index >= 0; index-- {
			if filtered[index].Successful() {
				if result.ConsecutiveFailures > 0 {
					break
				}
				result.ConsecutiveSuccesses++
			} else {
				if result.ConsecutiveSuccesses > 0 {
					break
				}
				result.ConsecutiveFailures++
			}
		}
	}
	if result.Successes > 0 {
		result.MeanSuccessDuration = total / time.Duration(result.Successes)
	}
	return result, nil
}

func sameKey(left, right Key) bool {
	return left.connection.id == right.connection.id && left.connection.revision.Equal(right.connection.revision) &&
		left.target.id == right.target.id && left.target.revision.Equal(right.target.revision) &&
		left.vantage == right.vantage && left.kind == right.kind && left.profile == right.profile
}

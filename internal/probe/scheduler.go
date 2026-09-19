package probe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
)

const (
	MaxSchedulerConcurrency = 64
	MaxSchedulerQueue       = 4096
	MaxSchedulerJobs        = 10_000
)

var ErrSchedule = errors.New("probe scheduling failed")

type Runner interface {
	Execute(context.Context, endpoint.Record, observation.HTTPTarget) (observation.Observation, error)
}

type ScheduleConfig struct {
	Concurrency int
	Queue       int
	PerEndpoint int
	PerTarget   int
	MaxJobs     int
}

func DefaultScheduleConfig() ScheduleConfig {
	return ScheduleConfig{Concurrency: 4, Queue: 256, PerEndpoint: 1, PerTarget: 2, MaxJobs: MaxSchedulerJobs}
}

func (config ScheduleConfig) valid() bool {
	return config.Concurrency >= 1 && config.Concurrency <= MaxSchedulerConcurrency &&
		config.Queue >= 1 && config.Queue <= MaxSchedulerQueue &&
		config.PerEndpoint >= 1 && config.PerEndpoint <= config.Concurrency &&
		config.PerTarget >= 1 && config.PerTarget <= config.Concurrency &&
		config.MaxJobs >= 1 && config.MaxJobs <= MaxSchedulerJobs
}

type Job struct {
	Record endpoint.Record
	Target observation.HTTPTarget
}

type Result struct {
	Observation observation.Observation
	Err         error
}

type Stats struct {
	Jobs                int
	PeakRunning         int
	PeakEndpointRunning int
	PeakTargetRunning   int
}

type Scheduler struct {
	runner Runner
	config ScheduleConfig
}

func NewScheduler(runner Runner, config ScheduleConfig) (*Scheduler, error) {
	if runner == nil || !config.valid() {
		return nil, scheduleFailure("configuration")
	}
	return &Scheduler{runner: runner, config: config}, nil
}

type indexedJob struct {
	index int
	job   Job
}

func (scheduler *Scheduler) Run(ctx context.Context, jobs []Job) ([]Result, Stats, error) {
	if len(jobs) > scheduler.config.MaxJobs {
		return nil, Stats{}, scheduleFailure("job_limit")
	}
	results := make([]Result, len(jobs))
	stats := Stats{Jobs: len(jobs)}
	if len(jobs) == 0 {
		return results, stats, nil
	}
	endpointLimits := make(map[string]chan struct{})
	targetLimits := make(map[string]chan struct{})
	for _, job := range jobs {
		endpointKey := job.Record.ID().String()
		targetKey := job.Target.ID().String()
		if endpointKey == "<invalid-endpoint-id>" || targetKey == "" {
			return nil, Stats{}, scheduleFailure("job")
		}
		if endpointLimits[endpointKey] == nil {
			endpointLimits[endpointKey] = make(chan struct{}, scheduler.config.PerEndpoint)
		}
		if targetLimits[targetKey] == nil {
			targetLimits[targetKey] = make(chan struct{}, scheduler.config.PerTarget)
		}
	}
	queue := make(chan indexedJob, scheduler.config.Queue)
	var wait sync.WaitGroup
	var running, peakRunning atomic.Int64
	var endpointPeaks, targetPeaks sync.Map
	workers := scheduler.config.Concurrency
	if workers > len(jobs) {
		workers = len(jobs)
	}
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for item := range queue {
				endpointKey := item.job.Record.ID().String()
				targetKey := item.job.Target.ID().String()
				if !acquire(ctx, endpointLimits[endpointKey]) {
					results[item.index].Err = scheduleFailure("cancelled")
					continue
				}
				if !acquire(ctx, targetLimits[targetKey]) {
					<-endpointLimits[endpointKey]
					results[item.index].Err = scheduleFailure("cancelled")
					continue
				}
				current := running.Add(1)
				updatePeak(&peakRunning, current)
				endpointCounter := loadCounter(&endpointPeaks, endpointKey)
				targetCounter := loadCounter(&targetPeaks, targetKey)
				endpointCurrent := endpointCounter.current.Add(1)
				targetCurrent := targetCounter.current.Add(1)
				updatePeak(&endpointCounter.peak, endpointCurrent)
				updatePeak(&targetCounter.peak, targetCurrent)
				value, err := scheduler.runner.Execute(ctx, item.job.Record, item.job.Target)
				results[item.index] = Result{Observation: value, Err: err}
				targetCounter.current.Add(-1)
				endpointCounter.current.Add(-1)
				running.Add(-1)
				<-targetLimits[targetKey]
				<-endpointLimits[endpointKey]
			}
		}()
	}
	for index, job := range jobs {
		select {
		case queue <- indexedJob{index: index, job: job}:
		case <-ctx.Done():
			results[index].Err = scheduleFailure("cancelled")
			for remaining := index + 1; remaining < len(jobs); remaining++ {
				results[remaining].Err = scheduleFailure("cancelled")
			}
			close(queue)
			wait.Wait()
			stats.PeakRunning = int(peakRunning.Load())
			stats.PeakEndpointRunning = maxPeak(&endpointPeaks)
			stats.PeakTargetRunning = maxPeak(&targetPeaks)
			return results, stats, nil
		}
	}
	close(queue)
	wait.Wait()
	stats.PeakRunning = int(peakRunning.Load())
	stats.PeakEndpointRunning = maxPeak(&endpointPeaks)
	stats.PeakTargetRunning = maxPeak(&targetPeaks)
	return results, stats, nil
}

type counters struct{ current, peak atomic.Int64 }

func loadCounter(values *sync.Map, key string) *counters {
	value, _ := values.LoadOrStore(key, &counters{})
	return value.(*counters)
}

func acquire(ctx context.Context, semaphore chan struct{}) bool {
	select {
	case semaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func updatePeak(peak *atomic.Int64, value int64) {
	for current := peak.Load(); value > current; current = peak.Load() {
		if peak.CompareAndSwap(current, value) {
			return
		}
	}
}

func maxPeak(values *sync.Map) int {
	maximum := int64(0)
	values.Range(func(_, value any) bool {
		if peak := value.(*counters).peak.Load(); peak > maximum {
			maximum = peak
		}
		return true
	})
	return int(maximum)
}

type ScheduleError struct{ code string }

func scheduleFailure(code string) error { return &ScheduleError{code: code} }
func (failure *ScheduleError) Error() string {
	return fmt.Sprintf("probe scheduling failed code=%s", failure.code)
}
func (failure *ScheduleError) Unwrap() error { return ErrSchedule }
func (failure *ScheduleError) Code() string  { return failure.code }

package selection

import (
	"errors"
	"time"
)

// Frame is one immutable evidence snapshot in a deterministic strategy replay.
type Frame struct {
	At         time.Time
	Candidates []Candidate
}

// Scenario supplies identical ordered evidence frames to each strategy.
type Scenario struct {
	Scope   string
	Context Context
	Policy  Policy
	Frames  []Frame
}

// Report contains bounded comparison measures. Latency is the mean of the
// selected candidates' M4 mean-success durations, not application traffic.
type Report struct {
	Strategy            Strategy
	Evaluations         int
	Changes             int
	EmergencyChanges    int
	NoEligible          int
	Degraded            int
	MeanSelectedLatency time.Duration
}

func Compare(scenario Scenario, strategies ...Strategy) ([]Report, error) {
	if len(strategies) == 0 || len(scenario.Frames) == 0 {
		return nil, errors.New("selection replay requires strategies and frames")
	}
	reports := make([]Report, len(strategies))
	for index, strategy := range strategies {
		policy := scenario.Policy
		policy.Strategy = strategy
		var state *State
		var latencyTotal time.Duration
		var latencySamples int
		report := Report{Strategy: strategy}
		for _, frame := range scenario.Frames {
			decision, err := Select(scenario.Scope, scenario.Context, policy, frame.Candidates, state, frame.At)
			if err != nil {
				return nil, err
			}
			report.Evaluations++
			if decision.Changed {
				report.Changes++
			}
			if decision.Transition == TransitionEmergency {
				report.EmergencyChanges++
			}
			if len(decision.Selected) == 0 {
				report.NoEligible++
			}
			if decision.Degraded {
				report.Degraded++
			}
			for _, selected := range decision.Selected {
				for _, explanation := range decision.Explanations {
					if explanation.EndpointID == selected.ID() && explanation.Reason == ReasonEligible {
						latencyTotal += explanation.MeanSuccessDuration
						latencySamples++
						break
					}
				}
			}
			next := decision.Next
			state = &next
		}
		if latencySamples > 0 {
			report.MeanSelectedLatency = latencyTotal / time.Duration(latencySamples)
		}
		reports[index] = report
	}
	return reports, nil
}

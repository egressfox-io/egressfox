// Package selection turns revision-specific observation facts into deterministic
// endpoint decisions. It performs no I/O and reads no ambient clock.
package selection

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/egressfox-io/egressfox/internal/artifact"
	"github.com/egressfox-io/egressfox/internal/endpoint"
	"github.com/egressfox-io/egressfox/internal/observation"
)

const algorithmVersion = "adaptive/v1"

type Strategy uint8

const (
	StrategyStatic Strategy = iota + 1
	StrategyLowestLatency
	StrategyAdaptive
)

func (strategy Strategy) String() string {
	switch strategy {
	case StrategyStatic:
		return "static"
	case StrategyLowestLatency:
		return "lowest_latency"
	case StrategyAdaptive:
		return "adaptive"
	default:
		return "unknown"
	}
}

type Policy struct {
	Strategy                    Strategy
	TopN                        int
	EvidenceWindow              time.Duration
	Freshness                   time.Duration
	MinSamples                  int
	MinSuccessPermille          int
	FailureStreak               int
	RecoverySuccesses           int
	Residence                   time.Duration
	Cooldown                    time.Duration
	RequiredImprovementPermille int
}

func DefaultPolicy(strategy Strategy) Policy {
	return Policy{Strategy: strategy, TopN: 1, EvidenceWindow: 30 * time.Minute,
		Freshness: 5 * time.Minute, MinSamples: 3, MinSuccessPermille: 600,
		FailureStreak: 2, RecoverySuccesses: 3, Residence: 10 * time.Minute,
		Cooldown: 15 * time.Minute, RequiredImprovementPermille: 100}
}

func (policy Policy) Validate() error {
	if policy.Strategy < StrategyStatic || policy.Strategy > StrategyAdaptive {
		return errors.New("invalid selection strategy")
	}
	if policy.TopN < 1 || policy.TopN > 10_000 || policy.MinSamples < 1 || policy.MinSamples > 512 ||
		policy.MinSuccessPermille < 1 || policy.MinSuccessPermille > 1000 ||
		policy.FailureStreak < 1 || policy.FailureStreak > 512 || policy.RecoverySuccesses < 1 || policy.RecoverySuccesses > 512 ||
		policy.EvidenceWindow <= 0 || policy.EvidenceWindow > 30*24*time.Hour || policy.Freshness <= 0 || policy.Freshness > policy.EvidenceWindow ||
		policy.Residence < 0 || policy.Residence > 7*24*time.Hour || policy.Cooldown < 0 || policy.Cooldown > 30*24*time.Hour ||
		policy.RequiredImprovementPermille < 0 || policy.RequiredImprovementPermille >= 1000 {
		return errors.New("invalid selection policy bounds")
	}
	return nil
}

type Context struct {
	Target  observation.TargetRef
	Vantage observation.VantageID
	Kind    observation.Kind
	Profile artifact.Profile
}

func NewContext(target observation.TargetRef, vantage observation.VantageID, kind observation.Kind, profile artifact.Profile) (Context, error) {
	// A dummy valid connection is deliberately unnecessary: the individual
	// summaries validate their complete keys. Validate the public dimensions here.
	if target.ID().String() == "" || vantage.String() == "" || kind.String() == "unknown" || profile.Engine.String() == "unknown" {
		return Context{}, errors.New("invalid selection context")
	}
	if _, err := target.Revision().RevealForPersistence(); err != nil {
		return Context{}, errors.New("invalid selection context")
	}
	return Context{Target: target, Vantage: vantage, Kind: kind, Profile: profile}, nil
}

func (context Context) Matches(key observation.Key) bool {
	return context.Target.Equal(key.Target()) && context.Vantage == key.Vantage() && context.Kind == key.Kind() && context.Profile == key.Profile()
}

type Candidate struct {
	Record   endpoint.Record
	Evidence *observation.Summary
}

type Member struct {
	Connection observation.ConnectionRef
	SelectedAt time.Time
}

type Cooldown struct {
	Connection observation.ConnectionRef
	Until      time.Time
}

type State struct {
	Scope             string
	Context           Context
	PolicyFingerprint [sha256.Size]byte
	EvaluatedAt       time.Time
	Members           []Member
	Cooldowns         []Cooldown
}

func (state State) String() string {
	return fmt.Sprintf("selection state scope=%s members=%d cooldowns=%d evaluated_at=%s context_revisions=<private>", state.Scope, len(state.Members), len(state.Cooldowns), state.EvaluatedAt.UTC().Format(time.RFC3339Nano))
}
func (State) MarshalJSON() ([]byte, error) {
	return nil, errors.New("selection state JSON serialization is disabled")
}

func (state State) Validate() error {
	if !safeScope(state.Scope) {
		return errors.New("invalid selection state scope")
	}
	if _, err := NewContext(state.Context.Target, state.Context.Vantage, state.Context.Kind, state.Context.Profile); err != nil {
		return err
	}
	zero := true
	for _, value := range state.PolicyFingerprint {
		zero = zero && value == 0
	}
	if zero {
		return errors.New("invalid selection policy fingerprint")
	}
	if state.EvaluatedAt.IsZero() {
		return errors.New("invalid selection evaluation time")
	}
	seen := make(map[string]struct{}, len(state.Members))
	for _, member := range state.Members {
		if member.SelectedAt.IsZero() {
			return errors.New("invalid selection member time")
		}
		key := refKey(member.Connection)
		if _, ok := seen[key]; ok {
			return errors.New("duplicate selection member")
		}
		seen[key] = struct{}{}
	}
	for _, cooldown := range state.Cooldowns {
		if cooldown.Until.IsZero() {
			return errors.New("invalid selection cooldown time")
		}
		if _, err := cooldown.Connection.Revision().RevealForPersistence(); err != nil {
			return errors.New("invalid selection cooldown connection")
		}
	}
	return nil
}

type Reason string

const (
	ReasonEligible            Reason = "eligible"
	ReasonMissingEvidence     Reason = "missing_evidence"
	ReasonWrongContext        Reason = "wrong_context"
	ReasonWrongRevision       Reason = "wrong_revision"
	ReasonStale               Reason = "stale"
	ReasonInsufficientSamples Reason = "insufficient_samples"
	ReasonUnreliable          Reason = "unreliable"
	ReasonFailureStreak       Reason = "failure_streak"
	ReasonNoSuccessfulLatency Reason = "no_successful_latency"
	ReasonCooldown            Reason = "cooldown"
	ReasonRecovery            Reason = "recovery"
)

type Explanation struct {
	EndpointID          endpoint.ID
	Reason              Reason
	Samples             int
	Successes           int
	SuccessPermille     int
	MeanSuccessDuration time.Duration
	ConfidencePPM       int64
	AdaptiveCostNanos   int64
	Incumbent           bool
	ResidenceActive     bool
}

func (explanation Explanation) String() string {
	return fmt.Sprintf("selection candidate endpoint=%s reason=%s samples=%d successes=%d confidence_ppm=%d cost_ns=%d incumbent=%t residence=%t revisions=<private>",
		explanation.EndpointID, explanation.Reason, explanation.Samples, explanation.Successes,
		explanation.ConfidencePPM, explanation.AdaptiveCostNanos, explanation.Incumbent, explanation.ResidenceActive)
}
func (Explanation) MarshalJSON() ([]byte, error) {
	return nil, errors.New("selection explanation JSON serialization is disabled")
}

type Transition string

const (
	TransitionInitial      Transition = "initial"
	TransitionUnchanged    Transition = "unchanged"
	TransitionOptimized    Transition = "optimized"
	TransitionEmergency    Transition = "emergency"
	TransitionContextReset Transition = "context_reset"
	TransitionNoEligible   Transition = "no_eligible"
)

type Decision struct {
	Selected     []endpoint.Record
	Explanations []Explanation
	Next         State
	Changed      bool
	Degraded     bool
	Transition   Transition
	Eligible     int
	Requested    int
}

func (decision Decision) String() string {
	return fmt.Sprintf("selection decision selected=%d eligible=%d requested=%d changed=%t degraded=%t transition=%s", len(decision.Selected), decision.Eligible, decision.Requested, decision.Changed, decision.Degraded, decision.Transition)
}
func (Decision) MarshalJSON() ([]byte, error) {
	return nil, errors.New("selection decision JSON serialization is disabled")
}

type ranked struct {
	candidate   Candidate
	ref         observation.ConnectionRef
	explanation Explanation
}

func Select(scope string, context Context, policy Policy, candidates []Candidate, previous *State, now time.Time) (Decision, error) {
	if !safeScope(scope) || now.IsZero() {
		return Decision{}, errors.New("selection requires a safe scope and explicit evaluation time")
	}
	if err := policy.Validate(); err != nil {
		return Decision{}, err
	}
	now = now.UTC()
	fingerprint := policyFingerprint(policy)
	usablePrevious := previous != nil && previous.Scope == scope && contextsEqual(previous.Context, context) && previous.PolicyFingerprint == fingerprint
	previousMembers := []Member(nil)
	cooldowns := []Cooldown(nil)
	transition := TransitionInitial
	if usablePrevious {
		if now.Before(previous.EvaluatedAt) {
			return Decision{}, errors.New("selection evaluation is older than committed state")
		}
		previousMembers = append(previousMembers, previous.Members...)
		cooldowns = append(cooldowns, previous.Cooldowns...)
		transition = TransitionUnchanged
	} else if previous != nil {
		transition = TransitionContextReset
	}

	ordered := append([]Candidate(nil), candidates...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Record.Identity().Compare(ordered[j].Record.Identity()) < 0 })
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].Record.Identity().Equal(ordered[index].Record.Identity()) {
			return Decision{}, errors.New("selection candidates contain duplicate connection revisions")
		}
	}

	rankedValues := make([]ranked, 0, len(ordered))
	explanations := make([]Explanation, 0, len(ordered))
	present := make(map[string]observation.ConnectionRef, len(ordered))
	for _, candidate := range ordered {
		ref, err := observation.NewConnectionRef(candidate.Record.Identity())
		if err != nil {
			return Decision{}, errors.New("selection candidate has invalid identity")
		}
		present[refKey(ref)] = ref
		incumbent, selectedAt := findMember(previousMembers, ref)
		explanation := evaluate(candidate, ref, context, policy, cooldowns, incumbent, selectedAt, now)
		explanations = append(explanations, explanation)
		if explanation.Reason == ReasonEligible {
			rankedValues = append(rankedValues, ranked{candidate: candidate, ref: ref, explanation: explanation})
		}
	}
	sortRanked(rankedValues, policy.Strategy)

	// Preserve only current-inventory cooldowns and release recovered revisions.
	nextCooldowns := make([]Cooldown, 0, len(cooldowns)+len(previousMembers))
	for _, cooldown := range cooldowns {
		if _, ok := present[refKey(cooldown.Connection)]; !ok {
			continue
		}
		entry := findRanked(rankedValues, cooldown.Connection)
		if now.Before(cooldown.Until) || entry == nil || entry.candidate.Evidence == nil || entry.candidate.Evidence.ConsecutiveSuccesses < policy.RecoverySuccesses {
			nextCooldowns = append(nextCooldowns, cooldown)
		}
	}

	selected := make([]ranked, 0, policy.TopN)
	emergency := false
	if policy.Strategy == StrategyAdaptive && usablePrevious {
		for _, member := range previousMembers {
			entry := findRanked(rankedValues, member.Connection)
			if entry != nil {
				selected = append(selected, *entry)
				continue
			}
			emergency = true
			reason := explanationReason(explanations, member.Connection.ID())
			if reason == ReasonFailureStreak || reason == ReasonUnreliable {
				nextCooldowns = upsertCooldown(nextCooldowns, Cooldown{Connection: member.Connection, Until: now.Add(policy.Cooldown)})
			}
		}
		if len(selected) > policy.TopN {
			sortRanked(selected, policy.Strategy)
			selected = selected[:policy.TopN]
		}
	}

	for _, candidate := range rankedValues {
		if len(selected) >= policy.TopN || containsRanked(selected, candidate.ref) {
			continue
		}
		selected = append(selected, candidate)
	}
	if policy.Strategy != StrategyAdaptive || !usablePrevious {
		if len(selected) > policy.TopN {
			selected = selected[:policy.TopN]
		}
	} else if len(selected) == policy.TopN {
		selected = optimize(selected, rankedValues, previousMembers, policy, now)
	}

	nextMembers := make([]Member, len(selected))
	selectedRecords := make([]endpoint.Record, len(selected))
	for index, value := range selected {
		_, selectedAt := findMember(previousMembers, value.ref)
		if selectedAt.IsZero() {
			selectedAt = now
		}
		nextMembers[index] = Member{Connection: value.ref, SelectedAt: selectedAt}
		selectedRecords[index] = value.candidate.Record
	}
	changed := !sameMembers(previousMembers, nextMembers)
	if len(selected) == 0 {
		transition = TransitionNoEligible
	} else if emergency && changed {
		transition = TransitionEmergency
	} else if changed && usablePrevious {
		transition = TransitionOptimized
	}
	return Decision{Selected: selectedRecords, Explanations: explanations,
		Next:    State{Scope: scope, Context: context, PolicyFingerprint: fingerprint, EvaluatedAt: now, Members: nextMembers, Cooldowns: nextCooldowns},
		Changed: changed, Degraded: len(selected) < policy.TopN, Transition: transition,
		Eligible: len(rankedValues), Requested: policy.TopN}, nil
}

func evaluate(candidate Candidate, ref observation.ConnectionRef, context Context, policy Policy, cooldowns []Cooldown, incumbent bool, selectedAt, now time.Time) Explanation {
	value := Explanation{EndpointID: candidate.Record.ID(), Reason: ReasonEligible, Incumbent: incumbent,
		ResidenceActive: incumbent && now.Before(selectedAt.Add(policy.Residence))}
	if candidate.Evidence == nil {
		value.Reason = ReasonMissingEvidence
		return value
	}
	summary := candidate.Evidence
	value.Samples, value.Successes = summary.Samples, summary.Successes
	if summary.Samples > 0 {
		value.SuccessPermille = summary.Successes * 1000 / summary.Samples
	}
	value.MeanSuccessDuration = summary.MeanSuccessDuration
	if !context.Matches(summary.Key) {
		value.Reason = ReasonWrongContext
		return value
	}
	if !summary.Key.Connection().Equal(ref) {
		value.Reason = ReasonWrongRevision
		return value
	}
	if !summary.Fresh {
		value.Reason = ReasonStale
		return value
	}
	if summary.Samples < policy.MinSamples {
		value.Reason = ReasonInsufficientSamples
		return value
	}
	if value.SuccessPermille < policy.MinSuccessPermille {
		value.Reason = ReasonUnreliable
		return value
	}
	if summary.ConsecutiveFailures >= policy.FailureStreak {
		value.Reason = ReasonFailureStreak
		return value
	}
	if summary.Successes == 0 || summary.MeanSuccessDuration <= 0 {
		value.Reason = ReasonNoSuccessfulLatency
		return value
	}
	if cooldown := findCooldown(cooldowns, ref); cooldown != nil {
		if now.Before(cooldown.Until) {
			value.Reason = ReasonCooldown
			return value
		}
		if summary.ConsecutiveSuccesses < policy.RecoverySuccesses {
			value.Reason = ReasonRecovery
			return value
		}
	}
	value.ConfidencePPM = wilsonLowerPPM(summary.Successes, summary.Samples)
	if value.ConfidencePPM <= 0 {
		value.Reason = ReasonUnreliable
		return value
	}
	value.AdaptiveCostNanos = riskCost(summary.MeanSuccessDuration, value.ConfidencePPM)
	return value
}

func sortRanked(values []ranked, strategy Strategy) {
	sort.SliceStable(values, func(i, j int) bool {
		var left, right int64
		switch strategy {
		case StrategyLowestLatency:
			left, right = values[i].explanation.MeanSuccessDuration.Nanoseconds(), values[j].explanation.MeanSuccessDuration.Nanoseconds()
		case StrategyAdaptive:
			left, right = values[i].explanation.AdaptiveCostNanos, values[j].explanation.AdaptiveCostNanos
		}
		if strategy != StrategyStatic && left != right {
			return left < right
		}
		return values[i].candidate.Record.Identity().Compare(values[j].candidate.Record.Identity()) < 0
	})
}

func optimize(selected, rankedValues []ranked, previous []Member, policy Policy, now time.Time) []ranked {
	for _, challenger := range rankedValues {
		if containsRanked(selected, challenger.ref) {
			continue
		}
		worst := -1
		for index, incumbent := range selected {
			_, since := findMember(previous, incumbent.ref)
			if !since.IsZero() && now.Before(since.Add(policy.Residence)) {
				continue
			}
			if worst < 0 || selected[worst].explanation.AdaptiveCostNanos < incumbent.explanation.AdaptiveCostNanos ||
				selected[worst].explanation.AdaptiveCostNanos == incumbent.explanation.AdaptiveCostNanos && selected[worst].candidate.Record.Identity().Compare(incumbent.candidate.Record.Identity()) < 0 {
				worst = index
			}
		}
		if worst < 0 {
			continue
		}
		if improvesBy(challenger.explanation, selected[worst].explanation, policy.RequiredImprovementPermille) {
			selected[worst] = challenger
		}
	}
	return selected
}

func improvesBy(challenger, incumbent Explanation, requiredPermille int) bool {
	// challenger duration/confidence <= incumbent duration/confidence *
	// (1000-required)/1000. big.Int preserves the exact public boundary
	// without overflow at the observation duration ceiling.
	left := new(big.Int).SetInt64(challenger.MeanSuccessDuration.Nanoseconds())
	left.Mul(left, big.NewInt(incumbent.ConfidencePPM))
	left.Mul(left, big.NewInt(1000))
	right := new(big.Int).SetInt64(incumbent.MeanSuccessDuration.Nanoseconds())
	right.Mul(right, big.NewInt(challenger.ConfidencePPM))
	right.Mul(right, big.NewInt(int64(1000-requiredPermille)))
	return left.Cmp(right) <= 0 && challenger.AdaptiveCostNanos < incumbent.AdaptiveCostNanos
}

func wilsonLowerPPM(successes, samples int) int64 {
	if samples <= 0 || successes < 0 || successes > samples {
		return 0
	}
	n, p, z := float64(samples), float64(successes)/float64(samples), 1.96
	z2 := z * z
	lower := (p + z2/(2*n) - z*math.Sqrt(p*(1-p)/n+z2/(4*n*n))) / (1 + z2/n)
	if lower < 0 {
		lower = 0
	}
	return int64(math.Round(lower * 1_000_000))
}

func riskCost(duration time.Duration, confidence int64) int64 {
	if duration <= 0 || confidence <= 0 {
		return math.MaxInt64
	}
	value := duration.Nanoseconds() * 1_000_000
	return (value + confidence - 1) / confidence
}

func policyFingerprint(policy Policy) [sha256.Size]byte {
	h := sha256.New()
	_, _ = h.Write([]byte(algorithmVersion))
	values := []int64{int64(policy.Strategy), int64(policy.TopN), int64(policy.EvidenceWindow), int64(policy.Freshness), int64(policy.MinSamples), int64(policy.MinSuccessPermille), int64(policy.FailureStreak), int64(policy.RecoverySuccesses), int64(policy.Residence), int64(policy.Cooldown), int64(policy.RequiredImprovementPermille)}
	var encoded [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(encoded[:], uint64(value))
		_, _ = h.Write(encoded[:])
	}
	var result [sha256.Size]byte
	copy(result[:], h.Sum(nil))
	return result
}

func contextsEqual(left, right Context) bool {
	return left.Target.Equal(right.Target) && left.Vantage == right.Vantage && left.Kind == right.Kind && left.Profile == right.Profile
}
func findMember(values []Member, ref observation.ConnectionRef) (bool, time.Time) {
	for _, value := range values {
		if value.Connection.Equal(ref) {
			return true, value.SelectedAt
		}
	}
	return false, time.Time{}
}
func findCooldown(values []Cooldown, ref observation.ConnectionRef) *Cooldown {
	for index := range values {
		if values[index].Connection.Equal(ref) {
			return &values[index]
		}
	}
	return nil
}
func findRanked(values []ranked, ref observation.ConnectionRef) *ranked {
	for index := range values {
		if values[index].ref.Equal(ref) {
			return &values[index]
		}
	}
	return nil
}
func containsRanked(values []ranked, ref observation.ConnectionRef) bool {
	return findRanked(values, ref) != nil
}
func sameMembers(left, right []Member) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].Connection.Equal(right[index].Connection) {
			return false
		}
	}
	return true
}
func explanationReason(values []Explanation, id endpoint.ID) Reason {
	for _, value := range values {
		if value.EndpointID == id {
			return value.Reason
		}
	}
	return ReasonMissingEvidence
}
func upsertCooldown(values []Cooldown, value Cooldown) []Cooldown {
	for index := range values {
		if values[index].Connection.Equal(value.Connection) {
			values[index] = value
			return values
		}
	}
	return append(values, value)
}
func refKey(ref observation.ConnectionRef) string {
	encoded, _ := ref.Revision().RevealForPersistence()
	return ref.ID().String() + string(encoded)
}
func safeScope(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '/' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxRecommendProbes limits how many tiny requests a recommendation tries.
const maxRecommendProbes = 8

// VerifyProbeTimeout is the per-model limit for a concurrent recommendation.
// The requests run together, so one slow model does not queue the rest.
// Once the newest candidate has answered, the rest are not held for this
// whole limit.
const VerifyProbeTimeout = 12 * time.Second

// settleAfterNewest is how long a recommendation keeps waiting after the
// newest candidate has answered, so a reply already in flight can still
// join the list. Anything still running after that is cancelled and left
// out of the result.
const settleAfterNewest = 250 * time.Millisecond

// Skip is a candidate that did not answer a recommendation probe.
type Skip struct {
	ID    string
	Check Check
}

// FirstWorking probes candidates with one tiny request each and returns the
// check that decided: the first success, a refused key, or the last failure.
// check.OK reports success, and check.Detail is the id that answered.
// Candidates run together, newest version first. The decision still walks
// that order: the first success, otherwise the first refused key (HTTP 401,
// 403, or a 4xx body that says the key is invalid), otherwise the last
// failure. A timeout is a skip. Other failures are returned in skipped.
func FirstWorking(ctx context.Context, opt ProbeOptions, models []Model, family, wire string) (check Check, skipped []Skip) {
	return workingCheck(ctx, opt, models, family, wire, opt.APIKey)
}

// workingCheck probes candidates and returns the first check that succeeded.
// The requests run together, so one timeout does not wait out the rest.
// Results are read in candidate order. The first success wins, including
// when an earlier id was refused, because a 403 can belong to one model.
// If nothing succeeded, the first refused key is the result. If every
// candidate fails some other way, the last failed check is returned.
// The caller must not save check.Detail as the user's
// model: it only proves the key and gateway answered.
func workingCheck(ctx context.Context, opt ProbeOptions, models []Model, family, wire, key string) (Check, []Skip) {
	spec := tinySpec(family, wire, key)
	order := cappedCandidates(models, family)
	if len(order) == 0 {
		return Check{Name: spec.name, Method: http.MethodPost, Path: spec.path(""), Hint: "No model answered a test request."}, nil
	}
	var last Check
	var skipped []Skip
	var rejected Check
	var rejectedSkips []Skip
	sawRejected := false
	for _, hit := range probeAll(ctx, opt, order, spec) {
		if hit.abandoned {
			continue
		}
		if hit.check.OK {
			return hit.check, skipped
		}
		if hit.check.Rejected() && !sawRejected {
			rejected = hit.check
			rejectedSkips = append([]Skip(nil), skipped...)
			sawRejected = true
		}
		skipped = append(skipped, Skip{ID: hit.id, Check: hit.check})
		last = hit.check
	}
	if sawRejected {
		return rejected, rejectedSkips
	}
	return last, skipped
}

// ProbeID sends one tiny request for a specific model id.
// It does not consult the catalog, so an id the user typed is still checked.
func ProbeID(ctx context.Context, opt ProbeOptions, family, wire, id string) Check {
	spec := tinySpec(family, wire, opt.APIKey)
	id = strings.TrimSpace(id)
	if id == "" {
		return Check{Name: spec.name, Method: http.MethodPost, Path: spec.path(""), Hint: "No model id."}
	}
	return postModel(ctx, probeClient(opt), opt, spec, id)
}

func probeClient(opt ProbeOptions) *http.Client {
	if opt.Timeout <= 0 {
		opt.Timeout = 20 * time.Second
	}
	if opt.Client != nil {
		return opt.Client
	}
	return newClient(opt.Timeout)
}

// VerifiedResult is a concurrent probe of one model family.
// Passed lists the ids that answered, in candidate order. Failures are
// dropped. Check is a success when any id answered, a 401 when the key was
// refused and nothing answered, a 403 when every probed id returned 403,
// or the last other failure.
type VerifiedResult struct {
	Passed  []string
	Skipped []Skip
	Check   Check
}

// VerifiedModels probes up to maxRecommendProbes candidates at once and
// returns only the ids that answered. A 403 on one model drops that model.
// It rejects the key only when nothing answered and a probe returned 401,
// or when every probed model returned 403.
func VerifiedModels(ctx context.Context, opt ProbeOptions, models []Model, family, wire string) VerifiedResult {
	spec := tinySpec(family, wire, opt.APIKey)
	order := cappedCandidates(models, family)
	if len(order) == 0 {
		return VerifiedResult{Check: Check{Name: spec.name, Method: http.MethodPost, Path: spec.path(""), Hint: "No model answered a test request."}}
	}
	results := probeAll(ctx, opt, order, spec)

	var passed []string
	var skipped []Skip
	var reject401, reject403, other Check
	saw401, saw403, sawOther := false, false, false
	for _, r := range results {
		if r.abandoned {
			continue
		}
		if r.check.OK {
			passed = append(passed, r.id)
			continue
		}
		skipped = append(skipped, Skip{ID: r.id, Check: r.check})
		switch r.check.Status {
		case http.StatusUnauthorized:
			if !saw401 {
				reject401 = r.check
			}
			saw401 = true
		case http.StatusForbidden:
			if !saw403 {
				reject403 = r.check
			}
			saw403 = true
		default:
			if !sawOther {
				other = r.check
			}
			sawOther = true
		}
	}
	out := VerifiedResult{Passed: passed, Skipped: skipped}
	switch {
	case len(passed) > 0:
		for _, r := range results {
			if r.abandoned {
				continue
			}
			if r.check.OK {
				out.Check = r.check
				break
			}
		}
	case saw401:
		out.Check = reject401
		out.Skipped = nil
	case saw403 && !sawOther:
		out.Check = reject403
		out.Skipped = nil
	default:
		out.Check = other
	}
	return out
}

type probeHit struct {
	id        string
	check     Check
	abandoned bool
}

func cappedCandidates(models []Model, family string) []string {
	order := CandidateOrder(models, family)
	if len(order) > maxRecommendProbes {
		order = order[:maxRecommendProbes]
	}
	return order
}

// probeAll sends one tiny request per id at the same time.
// A zero timeout uses VerifyProbeTimeout. Each request has that limit of
// its own. When the newest id (order[0]) has already answered, slower ids
// are cancelled after settleAfterNewest and reported as abandoned. A newest
// id that itself fails or times out does not cancel the others: an older
// id can still be the one that answers.
func probeAll(ctx context.Context, opt ProbeOptions, order []string, spec modelSpec) []probeHit {
	if len(order) == 0 {
		return nil
	}
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = VerifyProbeTimeout
	}
	client := probeClient(ProbeOptions{Timeout: timeout, Client: opt.Client})
	base, cancelBase := context.WithCancel(ctx)
	defer cancelBase()
	results := make([]probeHit, len(order))
	finished := make([]chan struct{}, len(order))
	var early atomic.Bool
	var wg sync.WaitGroup
	for i, id := range order {
		finished[i] = make(chan struct{})
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			defer close(finished[i])
			own, cancel := context.WithTimeout(base, timeout)
			defer cancel()
			check := postModel(own, client, opt, spec, id)
			hit := probeHit{id: id, check: check}
			// A real per-model timeout is DeadlineExceeded and stays a skip.
			// Cancel after the newest id answered is not a timeout.
			if !check.OK && check.Status == 0 && early.Load() && !errors.Is(own.Err(), context.DeadlineExceeded) {
				hit.abandoned = true
			}
			results[i] = hit
		}(i, id)
	}
	allDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(allDone)
	}()

	select {
	case <-finished[0]:
	case <-allDone:
	case <-ctx.Done():
		<-allDone
		return results
	}
	if results[0].check.OK {
		timer := time.NewTimer(settleAfterNewest)
		select {
		case <-allDone:
			timer.Stop()
		case <-timer.C:
			early.Store(true)
			cancelBase()
			<-allDone
		case <-ctx.Done():
			timer.Stop()
			cancelBase()
			<-allDone
		}
		return results
	}
	select {
	case <-allDone:
	case <-ctx.Done():
		<-allDone
	}
	return results
}

func tinySpec(family, wire, key string) modelSpec {
	family = strings.ToLower(strings.TrimSpace(family))
	switch family {
	case "claude":
		return messageSpec("messages", "", key)
	case "gemini":
		return geminiSpec("", key)
	case "grok":
		return chatSpec("grok", "grok", "", fallbackGrokModel, key)
	default:
		spec := chatSpec("chat", "gpt", "", fallbackChatModel, key)
		if strings.EqualFold(strings.TrimSpace(wire), "responses") {
			spec.kind = "responses"
		}
		return spec
	}
}

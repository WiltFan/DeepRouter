package gateway

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxRecommendProbes limits how many tiny requests a recommendation tries.
const maxRecommendProbes = 8

// VerifyProbeTimeout is the per-model limit for a concurrent recommendation.
// The requests run together, so the wait is one timeout, not the sum.
const VerifyProbeTimeout = 12 * time.Second

// Skip is a candidate that did not answer a recommendation probe.
type Skip struct {
	ID    string
	Check Check
}

// FirstWorking probes candidates with one tiny request each and returns the
// check that decided: the first success, a refused key, or the last failure.
// check.OK reports success, and check.Detail is the id that answered.
// Gateway-recommended ids are tried first. A refused key stops the scan,
// including HTTP 401, 403, and a 4xx body that says the key is invalid,
// because later models would fail the same way. Other failures are returned
// in skipped.
func FirstWorking(ctx context.Context, opt ProbeOptions, models []Model, family, wire string) (check Check, skipped []Skip) {
	return workingCheck(ctx, opt, models, family, wire, opt.APIKey)
}

// workingCheck probes candidates and returns the first check that succeeded.
// A refused key is returned immediately. If every candidate fails, the last
// failed check is returned. The caller must not save check.Detail as the
// user's model: it only proves the key and gateway answered.
func workingCheck(ctx context.Context, opt ProbeOptions, models []Model, family, wire, key string) (Check, []Skip) {
	spec := tinySpec(family, wire, key)
	order := CandidateOrder(models, family)
	if len(order) > maxRecommendProbes {
		order = order[:maxRecommendProbes]
	}
	if len(order) == 0 {
		return Check{Name: spec.name, Method: http.MethodPost, Path: spec.path(""), Hint: "No model answered a test request."}, nil
	}
	client := probeClient(opt)
	var last Check
	var skipped []Skip
	for _, candidate := range order {
		check := postModel(ctx, client, opt, spec, candidate)
		if check.OK || check.Rejected() {
			return check, skipped
		}
		skipped = append(skipped, Skip{ID: candidate, Check: check})
		last = check
		if ctx.Err() != nil {
			break
		}
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
	order := CandidateOrder(models, family)
	if len(order) > maxRecommendProbes {
		order = order[:maxRecommendProbes]
	}
	if len(order) == 0 {
		return VerifiedResult{Check: Check{Name: spec.name, Method: http.MethodPost, Path: spec.path(""), Hint: "No model answered a test request."}}
	}
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = VerifyProbeTimeout
	}
	client := probeClient(ProbeOptions{Timeout: timeout, Client: opt.Client})
	type one struct {
		id    string
		check Check
	}
	results := make([]one, len(order))
	var wg sync.WaitGroup
	for i, id := range order {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			results[i] = one{id: id, check: postModel(ctx, client, opt, spec, id)}
		}(i, id)
	}
	wg.Wait()

	var passed []string
	var skipped []Skip
	var reject401, reject403, other Check
	saw401, saw403, sawOther := false, false, false
	for _, r := range results {
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

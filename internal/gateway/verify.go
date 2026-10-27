package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// maxRecommendProbes limits how many tiny requests a recommendation tries.
const maxRecommendProbes = 8

// Skip is a candidate that did not answer a recommendation probe.
type Skip struct {
	ID    string
	Check Check
}

// FirstWorking probes candidates with one tiny request each and returns the
// check that decided: the first success, a 401 or 403, or the last failure.
// check.OK reports success, and check.Detail is the id that answered.
// Gateway-recommended ids are tried first. A 401 or 403 stops the scan: the
// key was refused, so later models would fail the same way. Other failures
// are returned in skipped.
func FirstWorking(ctx context.Context, opt ProbeOptions, models []Model, family, wire string) (check Check, skipped []Skip) {
	return workingCheck(ctx, opt, models, family, wire, opt.APIKey)
}

// workingCheck probes candidates and returns the first check that succeeded.
// A 401 or 403 is returned immediately. If every candidate fails, the last
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

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
	ID   string
	Hint string
}

// FirstWorking probes candidates with one tiny request and returns the first
// id that succeeds. Gateway-recommended ids are tried first. A 401 or 403
// stops the scan: the key was rejected, so later models would fail the same way.
// Other failures are skipped. An empty id means every probed model failed.
func FirstWorking(ctx context.Context, opt ProbeOptions, models []Model, family, wire string) (id string, auth bool, hint string, skipped []Skip) {
	check, skipped := workingCheck(ctx, opt, models, family, wire, opt.APIKey)
	if check.Status == http.StatusUnauthorized || check.Status == http.StatusForbidden {
		return "", true, check.Hint, skipped
	}
	if check.OK {
		return check.Detail, false, "", skipped
	}
	return "", false, check.Hint, skipped
}

// workingCheck probes candidates and returns the first check that succeeded.
// A 401 or 403 is returned immediately. If every candidate fails, the last
// failed check is returned. The caller must not save check.Detail as the
// user's model: it only proves the key and gateway answered.
func workingCheck(ctx context.Context, opt ProbeOptions, models []Model, family, wire, key string) (Check, []Skip) {
	order := CandidateOrder(models, family)
	if len(order) > maxRecommendProbes {
		order = order[:maxRecommendProbes]
	}
	if len(order) == 0 {
		return Check{OK: false, Hint: "no model answered"}, nil
	}
	client := probeClient(opt)
	spec := tinySpec(family, wire, key)
	var last Check
	var skipped []Skip
	for _, candidate := range order {
		check := postModel(ctx, client, opt, spec, candidate)
		if check.Status == http.StatusUnauthorized || check.Status == http.StatusForbidden {
			return check, skipped
		}
		if check.OK {
			return check, skipped
		}
		skipped = append(skipped, Skip{ID: candidate, Hint: check.Hint})
		last = check
	}
	return last, skipped
}

// ProbeID sends one tiny request for a specific model id.
// It does not consult the catalog, so an id the user typed is still checked.
func ProbeID(ctx context.Context, opt ProbeOptions, family, wire, id string) (ok bool, auth bool, hint string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, false, ""
	}
	client := probeClient(opt)
	check := postModel(ctx, client, opt, tinySpec(family, wire, opt.APIKey), id)
	if check.Status == http.StatusUnauthorized || check.Status == http.StatusForbidden {
		return false, true, check.Hint
	}
	return check.OK, false, check.Hint
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

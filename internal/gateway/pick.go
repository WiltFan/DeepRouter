package gateway

import "strings"

// FilterIDs returns ids that contain needle, case-insensitively, in the
// original order.
func FilterIDs(ids []string, needle string) []string {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return nil
	}
	var out []string
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), needle) {
			out = append(out, id)
		}
	}
	return out
}

// PickDefault chooses a model id from a gateway list.
//
// "claude" prefers a sonnet id, then opus, then fable, then any other id
// containing "claude". "gpt" prefers a gpt-5 id, then gpt-4o, then any other
// id containing "gpt" except gpt-4.1. gpt-4.1 is used only when it is the
// only gpt id: that id has no channel on the default gateway.
func PickDefault(ids []string, kind string) string {
	return PickDefaultModels(modelsFromIDs(ids), kind)
}

// PickDefaultModels chooses a model id from a gateway catalog.
// Non-chat models are skipped. See PickDefault for the family preferences.
// If every id of that family is non-chat, the choice falls back to that
// unfiltered family list.
func PickDefaultModels(models []Model, kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	matched := FamilyIDs(models, kind)
	if len(matched) == 0 {
		return ""
	}
	switch kind {
	case "claude":
		for _, pref := range []string{"sonnet", "opus", "fable"} {
			if id := firstContaining(matched, pref); id != "" {
				return id
			}
		}
		return matched[0]
	case "gpt":
		for _, pref := range []string{"gpt-5", "gpt-4o"} {
			if id := firstContaining(matched, pref); id != "" {
				return id
			}
		}
		for _, id := range matched {
			if !isGPT41(id) {
				return id
			}
		}
		return matched[0]
	default:
		return matched[0]
	}
}

func firstContaining(ids []string, needle string) string {
	needle = strings.ToLower(needle)
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), needle) {
			return id
		}
	}
	return ""
}

func isGPT41(id string) bool {
	low := strings.ToLower(id)
	return low == "gpt-4.1" || strings.HasPrefix(low, "gpt-4.1-") || strings.HasPrefix(low, "gpt-4.1.")
}

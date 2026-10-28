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
// The choice is the newest version in that family. GPT models older than
// gpt-5.4 are not chosen.
func PickDefault(ids []string, kind string) string {
	return PickDefaultModels(modelsFromIDs(ids), kind)
}

// PickDefaultModels chooses a model id from a gateway catalog.
// Non-chat models are skipped. If every id of that family is non-chat, the
// choice falls back to that unfiltered family list. The chosen id is the
// newest version. See FamilyIDs.
func PickDefaultModels(models []Model, kind string) string {
	matched := FamilyIDs(models, kind)
	if len(matched) == 0 {
		return ""
	}
	return matched[0]
}

// CandidateOrder is the order a recommendation probe should try.
// Ids are newest version first. A gateway recommendation does not jump
// ahead of a newer id. GPT models older than gpt-5.4 are omitted.
func CandidateOrder(models []Model, kind string) []string {
	return FamilyIDs(models, kind)
}

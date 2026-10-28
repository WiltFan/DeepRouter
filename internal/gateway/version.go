package gateway

import (
	"sort"
	"strconv"
	"strings"
)

// gptFloor is the oldest GPT version that stays in a candidate list.
// gpt-5.4 and gpt-5.4-mini qualify. gpt-5.3, gpt-5, and gpt-4.x do not.
var gptFloor = []int{5, 4}

// parseModelVersion returns the first version embedded in id.
// A version is a run of numbers of at most two digits, separated by '.' or
// '-'. A letter ends the run, so gpt-5.4-mini is 5.4 and gpt-4o is 4.
// A longer number is a date or a parameter count: it is skipped before a
// version starts, and it ends the version once one has started. That drops
// the date in gemini-2.5-flash-preview-09-2025 and claude-3-5-sonnet-20241022.
func parseModelVersion(id string) ([]int, bool) {
	s := strings.ToLower(id)
	var ver []int
	started := false
	for i := 0; i < len(s); {
		if s[i] < '0' || s[i] > '9' {
			if started {
				break
			}
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		token := s[i:j]
		n, err := strconv.Atoi(token)
		if err != nil || len(token) > 2 || n > 99 {
			if started {
				break
			}
			i = j
			continue
		}
		if started && i > 0 && s[i-1] != '.' && s[i-1] != '-' {
			break
		}
		started = true
		ver = append(ver, n)
		i = j
		if i < len(s) && (s[i] == '.' || s[i] == '-') && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			i++
			continue
		}
		break
	}
	if len(ver) == 0 {
		return nil, false
	}
	return ver, true
}

// compareVersion reports the order of two versions. A missing component is 0,
// so 5.4 is newer than 5, and 6 is newer than 5.4.
func compareVersion(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

// gptBelowFloor reports whether id is a GPT model older than gpt-5.4,
// or a GPT model with no version. Other families are never below the floor.
func gptBelowFloor(id string) bool {
	if !strings.Contains(strings.ToLower(id), "gpt") {
		return false
	}
	ver, ok := parseModelVersion(id)
	if !ok {
		return true
	}
	return compareVersion(ver, gptFloor) < 0
}

// variantRank breaks ties between equal versions. Sonnet, then opus, then
// fable, then any other name. It is only used when the versions match.
func variantRank(id string) int {
	low := strings.ToLower(id)
	switch {
	case strings.Contains(low, "sonnet"):
		return 0
	case strings.Contains(low, "opus"):
		return 1
	case strings.Contains(low, "fable"):
		return 2
	default:
		return 3
	}
}

// newerModel reports whether a should be tried before b.
// A versioned id comes before an unversioned one. A higher version comes
// first. Equal versions use variantRank. Otherwise the original order stands.
func newerModel(a, b string) bool {
	av, aok := parseModelVersion(a)
	bv, bok := parseModelVersion(b)
	if aok != bok {
		return aok
	}
	if !aok {
		return false
	}
	if cmp := compareVersion(av, bv); cmp != 0 {
		return cmp > 0
	}
	ar, br := variantRank(a), variantRank(b)
	if ar != br {
		return ar < br
	}
	return false
}

func sortNewest(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		return newerModel(ids[i], ids[j])
	})
}

// eligibleFamily drops GPT models below gpt-5.4 and sorts newest first.
func eligibleFamily(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || gptBelowFloor(id) {
			continue
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	sortNewest(out)
	return out
}

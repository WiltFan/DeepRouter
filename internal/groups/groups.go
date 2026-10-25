// Package groups lists the official-key groups a DeepRouter profile can select.
// A group is chosen by which API key the profile stores.
package groups

// Group is a recommended official-key profile.
type Group struct {
	Name  string
	Label string
	Blurb string
}

// Official is the set docs recommend as defaults.
var Official = []Group{
	{Name: "openai", Label: "OpenAI", Blurb: "Official OpenAI keys"},
	{Name: "azure", Label: "Azure", Blurb: "Azure OpenAI keys"},
	{Name: "aws", Label: "AWS", Blurb: "Amazon Bedrock keys"},
	{Name: "vertex", Label: "Vertex", Blurb: "Google Vertex keys"},
	{Name: "officially", Label: "Officially", Blurb: "Official provider keys"},
}

// Lookup returns a recommended group by name or label, case-insensitively.
func Lookup(name string) (Group, bool) {
	for _, g := range Official {
		if eq(g.Name, name) || eq(g.Label, name) {
			return g, true
		}
	}
	return Group{}, false
}

func eq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

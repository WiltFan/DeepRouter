package clientconfig

import (
	"fmt"
	"strings"

	"github.com/zfg5118/deeprouter/internal/gateway"
)

func Protocols(provider string) []string {
	switch provider {
	case "anthropic":
		return []string{gateway.AnthropicMessages}
	case "gemini":
		return []string{gateway.GoogleGenerativeAI}
	case "openai-api", "xai":
		return []string{gateway.OpenAICompletions, gateway.OpenAIResponses}
	default:
		return []string{gateway.OpenAICompletions, gateway.OpenAIResponses, gateway.AnthropicMessages, gateway.GoogleGenerativeAI}
	}
}

func ResolveProtocol(provider, input string) (string, error) {
	choices := Protocols(provider)
	if input == "" {
		return choices[0], nil
	}
	for _, protocol := range choices {
		if strings.TrimSpace(input) == protocol {
			return protocol, nil
		}
	}
	return "", fmt.Errorf("protocol must be one of: %s", strings.Join(choices, ", "))
}

func ModelID(protocol, model string) string {
	if protocol == gateway.GoogleGenerativeAI {
		for strings.HasPrefix(model, "models/") {
			model = strings.TrimPrefix(model, "models/")
		}
	}
	return model
}

func connectionBase(base, protocol string, pi bool) (string, error) {
	ep, err := gateway.ParseBase(base)
	if err != nil {
		return "", err
	}
	switch protocol {
	case gateway.AnthropicMessages:
		if pi {
			return ep.AnthropicBase, nil
		}
		return ep.AnthropicBase + "/v1", nil
	case gateway.GoogleGenerativeAI:
		return ep.GeminiBase() + "/v1beta", nil
	default:
		return ep.OpenAIBase, nil
	}
}

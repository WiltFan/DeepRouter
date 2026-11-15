package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zfg5118/deeprouter/internal/gateway"
)

// codexChatDiscussion is OpenAI's notice that Codex dropped Chat Completions.
const codexChatDiscussion = "https://github.com/openai/codex/discussions/7782"

// errCodexChatUnsupported is wire_api = "chat". Current Codex (0.95 and
// later; checked with 0.166.0) stops at config load with "`wire_api =
// "chat"` is no longer supported", so deeprouter refuses it before any
// file is written. It does not fall back to responses.
var errCodexChatUnsupported = errors.New("codex chat wire api is not supported")

// codexChatError is errCodexChatUnsupported with the explanation as its text.
func codexChatError() error {
	return chatWireError{codexChatLine()}
}

type chatWireError struct{ msg string }

func (e chatWireError) Error() string        { return e.msg }
func (e chatWireError) Is(target error) bool { return target == errCodexChatUnsupported }

func codexChatLine() string {
	return ui(
		`Chat Completions (wire_api = "chat") is not supported by current Codex. Codex 0.95 and later, including 0.166.0, refuses to load a config.toml with wire_api = "chat". Use responses (the default, POST /v1/responses). See `+codexChatDiscussion+`. Nothing was written.`,
		`当前 Codex 不支持 Chat Completions（wire_api = "chat"）。Codex 0.95 及以后的版本（包括 0.166.0）遇到 wire_api = "chat" 会拒绝加载 config.toml。请使用 responses（默认，POST /v1/responses）。详见 `+codexChatDiscussion+`。没有写入配置。`,
	)
}

// errModelIDInvalid is a typed or flag model id that cannot be a model id
// or would not be safe in a config file. It is a format error, not a test
// result.
var errModelIDInvalid = errors.New("invalid model id")

// maxModelIDLen bounds a typed or flag model id.
const maxModelIDLen = 200

// validateModelID checks a model id the user typed or passed with a flag.
// The gateway list does not matter: an unlisted id is allowed and is then
// tested. Rejected: empty, a yes/no/quit answer, whitespace, control
// characters (including newlines), invalid UTF-8, and over-long input.
func validateModelID(s string) error {
	if s == "" {
		return fmt.Errorf("%w: empty", errModelIDInvalid)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: not UTF-8", errModelIDInvalid)
	}
	if len(s) > maxModelIDLen {
		return fmt.Errorf("%w: too long", errModelIDInvalid)
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("%w: control character or space", errModelIDInvalid)
		}
	}
	if !plausibleModelID(s) {
		return fmt.Errorf("%w: not a model id", errModelIDInvalid)
	}
	return nil
}

// flagModelError is a --*-model value that failed validateModelID.
func flagModelError(flagName, value string, err error) error {
	return fmt.Errorf("%s", ui(
		fmt.Sprintf("--%s %q is not a valid model id (empty, a yes/no answer, spaces, or control characters). Nothing was written.", flagName, value),
		fmt.Sprintf("--%s %q 不是有效的模型 id（为空、是 yes/no 之类的回答，或含空格、控制字符）。没有写入配置。", flagName, value),
	))
}

// validateFlagModel checks an explicit model flag. "default" and an empty
// value keep their old meaning and are not ids.
func validateFlagModel(flagName, raw string, explicit bool) error {
	if !explicit {
		return nil
	}
	v := strings.TrimSpace(raw)
	if v == "" || strings.EqualFold(v, "default") {
		return nil
	}
	if err := validateModelID(raw); err != nil {
		// A surrounding space is trimmed, but a newline or control
		// character anywhere is refused.
		if strings.ContainsFunc(raw, unicode.IsControl) {
			return flagModelError(flagName, raw, err)
		}
		if err := validateModelID(v); err != nil {
			return flagModelError(flagName, raw, err)
		}
	}
	return nil
}

// modelPassedLine is printed only after id answered the probe on route.
func modelPassedLine(id, route string) string {
	return ui(
		fmt.Sprintf("%s answered a test request on %s.", id, route),
		fmt.Sprintf("%s 已通过测试（%s）。", id, route),
	)
}

// modelFailureLine says why id did not pass its probe. It separates a
// timeout, a rejected parameter, a route the gateway does not serve, and
// any other failure. It never says the model works.
func modelFailureLine(id string, check gateway.Check) string {
	route := strings.TrimSpace(check.Method + " " + check.Path)
	hint := closed(localHint(check))
	switch gateway.ClassifyFailure(check) {
	case gateway.FailureTimeout:
		return ui(
			fmt.Sprintf("Test failed: %s did not answer on %s before the timeout, or the gateway could not be reached. %s", id, route, hint),
			fmt.Sprintf("测试失败：%s 在超时前没有在 %s 上应答，或无法连接网关。%s", id, route, hint),
		)
	case gateway.FailureParameter:
		return ui(
			fmt.Sprintf("Parameter not supported: the gateway rejected a parameter of the %s test request for %s (for example reasoning.effort). %s The model was not confirmed.", route, id, hint),
			fmt.Sprintf("参数不支持：网关拒绝了 %s 测试请求中 %s 的某个参数（例如 reasoning.effort）。%s模型没有通过确认。", route, id, hint),
		)
	case gateway.FailureProtocol:
		return ui(
			fmt.Sprintf("Protocol not supported: the gateway does not serve %s for %s. %s The client calls this route, so this id cannot be used with it.", route, id, hint),
			fmt.Sprintf("协议不兼容：网关没有为 %s 提供 %s。%s客户端调用的就是这个接口，所以不能使用这个 id。", id, route, hint),
		)
	default:
		return ui(
			fmt.Sprintf("Test failed: %s did not answer a test request on %s. %s", id, route, hint),
			fmt.Sprintf("测试失败：%s 没有通过 %s 测试请求。%s", id, route, hint),
		)
	}
}

// modelUntestedLine is a custom id that could not be tested at all, so it
// is not saved.
func modelUntestedLine(id string) string {
	return ui(
		fmt.Sprintf("%s has not been tested, so it cannot be saved. A custom model id must answer a test request first.", id),
		fmt.Sprintf("%s 还没有测试，不能保存。自定义模型 id 必须先通过测试请求。", id),
	)
}

// probeOfficialModel tests one model id on an official API preset. Tests
// replace it so a fake key never leaves the process.
var probeOfficialModel = func(providerID, baseURL, key, family, wire, id string) gateway.Check {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return gateway.Check{Hint: err.Error(), Detail: id}
	}
	return gateway.ProbeOfficialID(ctx, providerID, gateway.ProbeOptions{
		Endpoints: ep,
		APIKey:    key,
		CodexWire: wire,
		Timeout:   20 * time.Second,
	}, family, wire, id)
}

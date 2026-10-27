package cli

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/zfg5118/deeprouter/internal/gateway"
)

// zhHints are the gateway package's fixed hints in Simplified Chinese.
// The gateway's own message is appended separately, unchanged.
var zhHints = map[string]string{
	"401 Bad key. The API key was rejected. Check the key on this profile.":                                               "401 令牌无效：网关拒绝了这个令牌，请检查令牌。",
	"403 Quota or permission. The gateway refused this request. Check this key's access.":                                 "403 额度不足或没有权限：网关拒绝了这个请求，请检查该令牌的额度和权限。",
	"404 Model or path. That model or path was not found. Run `deeprouter models` and switch to an id the gateway lists.": "404 模型或路径不存在：请运行 deeprouter models，改用网关列出的模型 id。",
	"503 No channel. No channel is available for this model right now. Try another model.":                                "503 没有可用渠道：这个模型当前没有可用渠道，请换一个模型。",
	"429 Rate limit. The gateway asked us to slow down. Wait a moment and run the test again.":                            "429 请求过于频繁：请稍等后再试。",
	"The gateway redirected the request. Check the base URL, including http versus https and a trailing /v1.":             "网关重定向了这个请求。请检查网关地址，包括 http 与 https，以及结尾的 /v1。",
	"The gateway returned an error.":                                                          "网关返回了错误。",
	"The model list could not be parsed.":                                                     "无法解析模型列表。",
	"Could not encode the request.":                                                           "无法生成请求。",
	"The request URL is not valid. Check the base URL.":                                       "请求地址无效，请检查网关地址。",
	"The response body could not be read.":                                                    "无法读取网关的响应。",
	"The gateway did not respond before the timeout. Check the base URL, or raise --timeout.": "网关在超时前没有响应。请检查网关地址，或调大 --timeout。",
	"TLS handshake failed. Confirm the base URL and that the gateway certificate is valid.":   "TLS 握手失败。请确认网关地址正确、证书有效。",
	"The connection was refused. Check the host, port, and that the gateway is running.":      "连接被拒绝。请检查主机、端口，以及网关是否在运行。",
	"DNS lookup failed. Check the host in the base URL.":                                      "域名解析失败。请检查网关地址里的主机名。",
	"Could not reach the gateway. Check the base URL and your network.":                       "无法连接网关。请检查网关地址和网络。",
	"No model answered a test request.":                                                       "没有模型通过测试请求。",
	"No model id.":                                                                            "没有模型 id。",
}

const skipHintPrefix = "Skipped. No "
const skipHintSuffix = " chat model in this gateway's model list."

// localHint is check.Hint in the UI language, followed by the gateway's own
// message when the hint does not already include it.
func localHint(check gateway.Check) string {
	hint := strings.TrimSpace(check.Hint)
	msg := strings.TrimSpace(check.Message)
	if !promptLocaleZH() {
		if msg != "" && !strings.Contains(hint, msg) {
			return strings.TrimSpace(hint + " " + msg)
		}
		return hint
	}
	base := hint
	if msg != "" {
		base = strings.TrimSpace(strings.TrimSuffix(base, msg))
	}
	zh, ok := zhHints[base]
	switch {
	case ok:
	case strings.HasPrefix(base, skipHintPrefix) && strings.HasSuffix(base, skipHintSuffix):
		family := strings.TrimSuffix(strings.TrimPrefix(base, skipHintPrefix), skipHintSuffix)
		zh = fmt.Sprintf("已跳过：这个网关的模型列表里没有 %s 对话模型。", family)
	case strings.HasPrefix(base, "HTTP ") && check.Status != 0:
		zh = fmt.Sprintf("HTTP %d：网关返回了错误。", check.Status)
	case base == "" && msg != "":
		zh = ""
	default:
		return hint
	}
	if msg != "" {
		zh += "网关返回：" + msg
	}
	return zh
}

// rejectionLine says that the gateway refused the token, with the gateway's
// own message. A 403 is usually an exhausted quota, not a mistyped token.
func rejectionLine(check gateway.Check) string {
	var line string
	if check.Status == http.StatusForbidden {
		line = ui(
			"The gateway refused this token (403): no quota left, or no access.",
			"网关拒绝了这个令牌（403）：额度不足或没有权限。",
		)
	} else {
		line = ui(
			fmt.Sprintf("The gateway rejected this token (%d).", check.Status),
			fmt.Sprintf("网关拒绝了这个令牌（%d）。", check.Status),
		)
	}
	if msg := strings.TrimSpace(check.Message); msg != "" {
		line += ui(" Gateway said: ", "网关返回：") + msg
	}
	return line
}

// closed ends s with a full stop in the UI language, so that another sentence
// can follow it. The gateway's own message often has no final punctuation.
func closed(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	if strings.ContainsRune(".!?。！？", r) {
		return s
	}
	return s + ui(".", "。")
}

// baseURLError is a gateway.ParseBase error in the UI language.
func baseURLError(err error) string {
	return localError(err.Error())
}

// modelCount is the "N models" detail of a model-list check.
func modelCount(detail string) string {
	var n int
	if _, err := fmt.Sscanf(detail, "%d models", &n); err == nil && promptLocaleZH() {
		return fmt.Sprintf("%d 个模型", n)
	}
	return detail
}

// pad appends spaces until s fills width terminal columns. Han characters
// and full-width punctuation take two columns. A longer s gets one space.
func pad(s string, width int) string {
	w := displayWidth(s)
	if w >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-w)
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		if wideRune(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func wideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe4f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}

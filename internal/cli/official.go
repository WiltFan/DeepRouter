package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/gateway"
)

// checkOfficialToken is the official-preset token check. Tests replace it
// so a fake key never leaves the process.
var checkOfficialToken = func(ctx context.Context, providerID, baseURL, apiKey string) (gateway.Check, error) {
	return gateway.CheckOfficial(ctx, providerID, baseURL, apiKey, nil)
}

// errOfficialRejected is a 401 or 403 from an official API. The wizard asks
// for the token again. setup turns it into an error and writes nothing.
var errOfficialRejected = errors.New("official token rejected")

func checkingTokenLine(baseURL string) string {
	return ui(
		fmt.Sprintf("Checking the token at %s ...", baseURL),
		fmt.Sprintf("正在确认 %s 的令牌……", baseURL),
	)
}

func officialRejectedLine(check gateway.Check) string {
	return ui(
		fmt.Sprintf("The token was rejected (%d).", check.Status),
		fmt.Sprintf("令牌被拒绝（%d）。", check.Status),
	)
}

func officialAcceptedShort(baseURL string) string {
	return ui(
		fmt.Sprintf("Token accepted at %s.", baseURL),
		fmt.Sprintf("令牌已被 %s 接受。", baseURL),
	)
}

// officialResultLine is the setup summary after a successful official check.
// A model flag is stored as given. An existing Codex model line is kept.
// Otherwise the client keeps its own default.
func officialResultLine(e env, baseURL string, checks []setupCheck) string {
	writing := false
	codex := false
	for _, c := range checks {
		if c.model != "" {
			writing = true
		}
		if c.family == "gpt" {
			codex = true
		}
	}
	if writing {
		return officialAcceptedShort(baseURL)
	}
	if codex {
		if kept := displayedCodexModel(e, ""); kept != "" {
			return ui(
				fmt.Sprintf("Token accepted at %s. The existing model %s is kept.", baseURL, kept),
				fmt.Sprintf("令牌已被 %s 接受。保留已有模型 %s。", baseURL, kept),
			)
		}
	}
	return ui(
		fmt.Sprintf("Token accepted at %s. No model was written; the client keeps its own default.", baseURL),
		fmt.Sprintf("令牌已被 %s 接受。没有写入模型，客户端使用自己的默认模型。", baseURL),
	)
}

func officialUnreachableLine(baseURL string, check gateway.Check) string {
	if check.Status == 0 {
		return ui(
			fmt.Sprintf("Could not reach %s, so the token was not checked. Official APIs need a network that can reach them. From mainland China that is usually a network outside China.", baseURL),
			fmt.Sprintf("无法连接 %s，没有确认令牌。官方接口需要能访问它们的网络。从中国大陆访问通常需要海外网络。", baseURL),
		)
	}
	return ui(
		fmt.Sprintf("%s returned HTTP %d, so the token was not checked. Official APIs need a network that can reach them. From mainland China that is usually a network outside China.", baseURL, check.Status),
		fmt.Sprintf("%s 返回了 HTTP %d，没有确认令牌。官方接口需要能访问它们的网络。从中国大陆访问通常需要海外网络。", baseURL, check.Status),
	)
}

func officialSkipPrompt() string {
	return ui(
		"Continue and write the config without checking the token? [y/N] ",
		"不检查令牌，继续写入配置？[y/N] ",
	)
}

// officialUnreachable warns that the official API did not confirm the token.
// skipVerify writes anyway. A terminal (or a wizard reader) asks first.
// Declining, or a non-interactive run without the flag, writes nothing.
// A 401 or 403 never reaches this function.
func (e env) officialUnreachable(in *bufio.Reader, baseURL string, check gateway.Check, skipVerify bool) error {
	fmt.Fprintln(e.stderr, officialUnreachableLine(baseURL, check))
	if skipVerify {
		fmt.Fprintln(e.stderr, ui(
			"Continuing because --skip-verify was set. The token was not checked.",
			"已设置 --skip-verify，继续写入。没有确认令牌。",
		))
		return nil
	}
	if in == nil && stdinIsTerminal(e.stdin) {
		in = bufio.NewReader(e.stdin)
	}
	if in != nil {
		answer, err := readLine(in, e.stderr, officialSkipPrompt())
		if err != nil {
			return err
		}
		if affirmed(answer) {
			fmt.Fprintln(e.stderr, ui(
				"The token was not checked. The config will still be written.",
				"没有确认令牌。配置仍会写入。",
			))
			return nil
		}
	}
	return fmt.Errorf("%s", ui(
		"Could not confirm the token, so nothing was written. Pass --skip-verify to write anyway.",
		"没有确认令牌，所以没有写入配置。加上 --skip-verify 可跳过检查并写入。",
	))
}

// verifyOfficial checks each distinct key before setup writes. A refused
// token writes nothing. A network failure asks, or fails unless skipVerify.
func (e env) verifyOfficial(providerID, baseURL string, checks []setupCheck, skipVerify bool) error {
	seen := map[string]bool{}
	skipped := false
	for _, c := range checks {
		if c.key == "" || seen[c.key] {
			continue
		}
		seen[c.key] = true
		fmt.Fprintln(e.stderr, checkingTokenLine(baseURL))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		check, err := checkOfficialToken(ctx, providerID, baseURL, c.key)
		cancel()
		if err != nil {
			return err
		}
		if check.OK {
			continue
		}
		if check.Rejected() {
			return fmt.Errorf("%s", closed(officialRejectedLine(check))+ui(" Nothing was written.", "没有写入配置。"))
		}
		if err := e.officialUnreachable(nil, baseURL, check, skipVerify); err != nil {
			return err
		}
		skipped = true
	}
	if len(seen) == 0 || skipped {
		return nil
	}
	fmt.Fprintln(e.stderr, officialResultLine(e, baseURL, checks))
	return nil
}

// confirmOfficial checks one wizard token. A refusal asks again. A network
// failure asks whether to continue. An accepted token is not checked again.
func (s *session) confirmOfficial(key string) error {
	if s.officialOK[key] {
		return nil
	}
	fmt.Fprintln(s.e.stderr, checkingTokenLine(s.baseURL))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	check, err := checkOfficialToken(ctx, s.provider, s.baseURL, key)
	cancel()
	if err != nil {
		return err
	}
	if check.OK {
		s.markOfficial(key)
		fmt.Fprintln(s.e.stderr, officialAcceptedShort(s.baseURL))
		return nil
	}
	if check.Rejected() {
		fmt.Fprintln(s.e.stderr, officialRejectedLine(check))
		return errOfficialRejected
	}
	if err := s.e.officialUnreachable(s.in, s.baseURL, check, false); err != nil {
		return err
	}
	s.markOfficial(key)
	return nil
}

func (s *session) markOfficial(key string) {
	if s.officialOK == nil {
		s.officialOK = map[string]bool{}
	}
	s.officialOK[key] = true
}

// officialModelLine is the wizard note for an official preset. Codex keeps
// an existing root model line; the sentence names that model.
func officialModelLine(e env, info apps.Info) string {
	if info.ID == apps.Codex {
		if kept := displayedCodexModel(e, ""); kept != "" {
			return ui(
				fmt.Sprintf("%s keeps its existing model %s.", info.Title, kept),
				fmt.Sprintf("%s 保留已有模型 %s。", info.Title, kept),
			)
		}
	}
	return ui(
		fmt.Sprintf("%s: no model set; the client uses its own default.", info.Title),
		fmt.Sprintf("%s 不指定模型，使用客户端默认。", info.Title),
	)
}

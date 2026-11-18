package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zfg5118/deeprouter/internal/apply"
	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/clientconfig"
	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/provider"
	"github.com/zfg5118/deeprouter/internal/store"
)

func resolveClientGateway(newClient bool, providerArg string, providerSet, baseSet bool, base string, existing store.Profile) (string, string, bool, error) {
	if newClient && providerSet && strings.EqualFold(strings.TrimSpace(providerArg), provider.CustomID) {
		if baseSet {
			return provider.CustomID, base, true, nil
		}
		if existing.Provider == provider.CustomID && existing.BaseURL != "" {
			return provider.CustomID, existing.BaseURL, true, nil
		}
		return "", "", false, fmt.Errorf("%s", ui("--provider custom requires --base-url", "--provider custom 需要 --base-url"))
	}
	return resolveGateway(providerArg, providerSet, baseSet, base, existing.Provider, existing.BaseURL)
}

func setClientProtocols(prof *store.Profile, set map[string]bool, openCode, pi string) error {
	for _, client := range []struct {
		id    apps.ID
		input string
		saved *string
	}{
		{apps.OpenCode, openCode, &prof.OpenCodeProtocol},
		{apps.Pi, pi, &prof.PiProtocol},
	} {
		flag := string(client.id) + "-protocol"
		if !set[flag] {
			continue
		}
		protocol, err := clientconfig.ResolveProtocol(provider.NormalizeID(prof.Provider), client.input)
		if err != nil {
			return fmt.Errorf("--%s: %w", flag, err)
		}
		*client.saved = protocol
	}
	return nil
}

func (e env) prepareClients(req *apply.Request, prof store.Profile) error {
	for _, id := range []apps.ID{apps.OpenCode, apps.Pi} {
		if (id == apps.OpenCode && !req.TouchOpenCode) || (id == apps.Pi && !req.TouchPi) {
			continue
		}
		info, _ := apps.ByID(id)
		if prog, ok := provider.ProgramFor(prof.Provider, id); ok && !prog.Exposed {
			return fmt.Errorf("%s", skipProgramLine(promptLocaleZH(), provider.DisplayName(prof.Provider), info.Title, prog.Route))
		}
		model, stored := prof.OpenCodeModel, prof.OpenCodeProtocol
		if id == apps.Pi {
			model, stored = prof.PiModel, prof.PiProtocol
		}
		protocol, err := clientconfig.ResolveProtocol(provider.NormalizeID(prof.Provider), stored)
		if err != nil {
			return fmt.Errorf("--%s-protocol: %w", id, err)
		}
		model = clientconfig.ModelID(protocol, model)
		if !validClientModel(id, model) {
			return fmt.Errorf("%s", ui(
				fmt.Sprintf("%s requires a concrete model ID. Pass --%s-model ID.", info.Title, id),
				fmt.Sprintf("%s 需要具体模型 ID。请传入 --%s-model ID。", info.Title, id),
			))
		}
		if id == apps.OpenCode {
			req.OpenCodeModel, req.OpenCodeProtocol = model, protocol
		} else {
			req.PiModel, req.PiProtocol = model, protocol
		}
	}
	if req.TouchOpenCode {
		path, err := clientconfig.OpenCodePath(e.home, os.Getenv("XDG_CONFIG_HOME"), os.Getenv("OPENCODE_CONFIG_DIR"), os.Getenv("OPENCODE_CONFIG"))
		if err != nil {
			return err
		}
		req.OpenCodePath, req.OpenCodeKey = path, prof.KeyFor(string(apps.OpenCode))
	}
	if req.TouchPi {
		dir, err := clientconfig.PiDir(e.home, os.Getenv("PI_CODING_AGENT_DIR"))
		if err != nil {
			return err
		}
		req.PiDir, req.PiKey = dir, prof.KeyFor(string(apps.Pi))
	}
	var paths []string
	if req.TouchOpenCode {
		paths = append(paths, req.OpenCodePath)
	}
	if req.TouchPi {
		paths = append(paths, piFiles(req.PiDir)...)
	}
	for _, path := range paths {
		if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("client configuration must be a regular file: %s", e.show(path))
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect client configuration: %s", e.show(path))
		}
	}
	return nil
}

func chatClientChecks(prof store.Profile, openCode, pi bool) ([]gateway.ChatClient, error) {
	var checks []gateway.ChatClient
	for _, client := range []struct {
		enabled              bool
		id                   apps.ID
		model, protocol, key string
	}{
		{openCode, apps.OpenCode, prof.OpenCodeModel, prof.OpenCodeProtocol, prof.OpenCodeKey},
		{pi, apps.Pi, prof.PiModel, prof.PiProtocol, prof.PiKey},
	} {
		if !client.enabled {
			continue
		}
		protocol, err := clientconfig.ResolveProtocol(provider.NormalizeID(prof.Provider), client.protocol)
		if err != nil {
			return nil, fmt.Errorf("--%s-protocol: %w", client.id, err)
		}
		checks = append(checks, gateway.ChatClient{Name: string(client.id), Model: clientconfig.ModelID(protocol, client.model), Key: client.key, Protocol: protocol})
	}
	return checks, nil
}

func piFiles(dir string) []string {
	return []string{
		filepath.Join(dir, clientconfig.PiModels),
		filepath.Join(dir, clientconfig.PiAuth),
		filepath.Join(dir, clientconfig.PiSettings),
	}
}

func clientReloadHint(id apps.ID) string {
	if id == apps.OpenCode {
		return ui("Start a new OpenCode CLI session or quit and reopen OpenCode Desktop.", "请新开 OpenCode CLI 会话，或退出并重新打开 OpenCode 桌面版。")
	}
	return ui("Start a new Pi session to use the saved default. Open /model to reload models in an existing session.", "请新开 Pi 会话以使用已保存的默认模型。在现有会话中打开 /model 可重读模型列表。")
}

func validClientModel(id apps.ID, model string) bool {
	return validateModelID(model) == nil && !strings.EqualFold(model, "default") && (id != apps.OpenCode || !strings.Contains(model, "#"))
}

func (s *session) askProtocol(info apps.Info) (string, error) {
	choices := clientconfig.Protocols(s.provider)
	if len(choices) == 1 {
		fmt.Fprintln(s.e.stderr, ui(info.Title+" protocol: ", info.Title+" 协议：")+choices[0])
		return choices[0], nil
	}
	for index, protocol := range choices {
		fmt.Fprintf(s.e.stderr, "  %d) %s\n", index+1, protocol)
	}
	for {
		line, err := readLine(s.in, s.e.stderr, ui(info.Title+" protocol [1]: ", info.Title+" 协议 [1]："))
		if err != nil {
			return "", err
		}
		if index, err := strconv.Atoi(line); err == nil && index >= 1 && index <= len(choices) {
			return choices[index-1], nil
		}
		if protocol, err := clientconfig.ResolveProtocol(s.provider, line); err == nil {
			return protocol, nil
		}
		fmt.Fprintln(s.e.stderr, ui("Choose a listed protocol.", "请选择列表中的协议。"))
	}
}

func (s *session) askClientModel(info apps.Info, key, protocol string) (string, error) {
	var ids []string
	if !provider.IsOfficial(s.provider) {
		models, cached := s.catalogs[key]
		if !cached {
			var err error
			models, _, err = fetchModels(s.baseURL, key, 30*time.Second)
			if err == nil {
				s.catalogs[key] = models
			}
		}
		seen := map[string]bool{}
		for _, candidate := range gateway.FilterChat(models) {
			if !matchesCatalogProtocol(candidate, protocol) {
				continue
			}
			model := clientconfig.ModelID(protocol, candidate.ID)
			if validClientModel(info.ID, model) && !seen[model] {
				ids = append(ids, model)
				seen[model] = true
			}
		}
	}
	if len(ids) > 0 {
		fmt.Fprintln(s.e.stderr, ui("Catalog suggestions. You can also enter an unlisted model ID.", "以下为目录建议，也可输入未列出的模型 ID。"))
		for index, model := range ids {
			fmt.Fprintf(s.e.stderr, "  %d) %s\n", index+1, model)
		}
	}
	for {
		id, err := readLine(s.in, s.e.stderr, ui(info.Title+" model ID: ", info.Title+" 模型 ID："))
		if err != nil {
			return "", err
		}
		if index, err := strconv.Atoi(id); len(ids) > 0 && err == nil && index >= 1 && index <= len(ids) {
			id = ids[index-1]
		}
		id = clientconfig.ModelID(protocol, strings.TrimSpace(id))
		if !validClientModel(info.ID, id) {
			fmt.Fprintln(s.e.stderr, ui("Enter a concrete model ID.", "请输入具体模型 ID。"))
			continue
		}
		check := probeOne(s.baseURL, key, protocol, "", id)
		if !check.OK {
			fmt.Fprintln(s.e.stderr, modelFailureLine(id, check))
			continue
		}
		return id, nil
	}
}

func matchesCatalogProtocol(model gateway.Model, protocol string) bool {
	for _, endpoint := range model.EndpointTypes {
		switch strings.ToLower(endpoint) {
		case "openai":
			if protocol == gateway.OpenAICompletions || protocol == gateway.OpenAIResponses {
				return true
			}
		case "openai-response", "openai-responses":
			if protocol == gateway.OpenAIResponses {
				return true
			}
		case "anthropic":
			if protocol == gateway.AnthropicMessages {
				return true
			}
		case "gemini":
			if protocol == gateway.GoogleGenerativeAI {
				return true
			}
		}
	}
	if len(model.EndpointTypes) > 0 {
		return false
	}
	switch protocol {
	case gateway.AnthropicMessages:
		return strings.Contains(strings.ToLower(model.ID), "claude")
	case gateway.GoogleGenerativeAI:
		return strings.Contains(strings.ToLower(model.ID), "gemini")
	default:
		return true
	}
}

**English** | [简体中文](README.zh-CN.md)

# deeprouter

One command to point the coding tools you choose at an OpenAI- and Anthropic-compatible API gateway.

![Terminal demo: install v0.3.2, set up Codex / ChatGPT, connectivity test passed](docs/demo.gif)

## Install

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex
```

cmd:

```bat
powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"
```

`curl ... | sh` in cmd fails with `'sh' is not recognized`. `install.sh` under Git Bash or MSYS exits and prints the PowerShell command above. Windows does not use `/dev/tty`.

The command is `deeprouter`. On macOS and Linux the binary is installed to `~/.local/bin/deeprouter`. When that directory is not on `PATH`, the installer prints an `export` line and writes it into the shell startup file (`~/.zshrc`, `~/.bashrc`, or the fish config). `--no-path` skips that step. On Windows the binary is installed to `%LOCALAPPDATA%\Programs\deeprouter\deeprouter.exe`, and that directory is added to the user PATH (registry `HKCU\Environment`, keeping the existing value type). `DEEPROUTER_NO_PATH=1` or `-NoPath` skips that step.

The previous command name was `deeprouter-setup`. The installer removes the old binary from the same directory and leaves a same-named entry that runs `deeprouter`: a `deeprouter-setup` script on macOS and Linux, and `deeprouter-setup.cmd` on Windows. The old `%LOCALAPPDATA%\Programs\deeprouter-setup` directory is removed from the user PATH.

After a successful install, a controlling terminal starts interactive setup immediately, using the absolute path of the binary just installed. `install.sh` reads from `/dev/tty` because the stdin of `curl | sh` is the install script itself. `install.ps1` starts the same wizard in the current Windows console, whose stdin is not the pipe. With no controlling terminal (CI and other non-interactive sessions) it does not start the wizard, and prints the absolute path to run next. `DEEPROUTER_NO_SETUP=1` skips the automatic start as well. That launch does not depend on `PATH` already being refreshed.

Install from a checkout:

```sh
git clone https://github.com/zfg5118/deeprouter.git
cd deeprouter
sh install.sh
```

From a Windows checkout:

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

`install.sh` is POSIX `sh` and covers macOS and Linux. `install.ps1` is the Windows installer. Next to a checkout, both compile a static binary with Go 1.22 or newer. The version baked into the binary is `git describe --tags`, or `dev` when there is no tag. An install from the network downloads a plain release binary (not an archive) and checks it against `checksums.txt` from the same place. The default host is `https://github.com/zfg5118/deeprouter/releases/latest/download/`. If that download fails and the repository is `zfg5118/deeprouter`, the installer tries `https://deeprouter.top/releases/latest/` (available once that mirror is deployed). `DEEPROUTER_DOWNLOAD_BASE` selects the first host. A mirror base uses `releases/<version-or-latest>/<asset>` rather than GitHub's `releases/download/` path. That step does not call the GitHub API. The file names are `deeprouter-darwin-amd64`, `deeprouter-darwin-arm64`, `deeprouter-linux-amd64`, `deeprouter-linux-arm64`, `deeprouter-windows-amd64.exe`, and `deeprouter-windows-arm64.exe`. `DEEPROUTER_SETUP_VERSION` without a leading `v` (for example `0.1.1`) pins GitHub to `releases/download/v0.1.1/` and a mirror to `releases/v0.1.1/`. After a fork, set `DEEPROUTER_SETUP_REPO=owner/name`. Forks do not fall back to `deeprouter.top` unless `DEEPROUTER_DOWNLOAD_BASE` is set. There is no runtime dependency after install.

## Mainland China

`raw.githubusercontent.com` and GitHub release downloads are often slow or blocked in mainland China. `https://deeprouter.top` can host the installer and the release files. These URLs are available once deployed.

macOS and Linux:

```sh
curl -fsSL https://deeprouter.top/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://deeprouter.top/install.ps1 | iex
```

cmd:

```bat
powershell -ExecutionPolicy Bypass -c "irm https://deeprouter.top/install.ps1 | iex"
```

The copy of the installer on GitHub tries GitHub first (short connect timeout), then `https://deeprouter.top`. The copy served by the mirror tries the mirror first, then GitHub. Either way the script prints which host it used, and it still checks `checksums.txt`. `DEEPROUTER_DOWNLOAD_BASE=https://deeprouter.top` forces the mirror first.

The server should serve the tree from `sh scripts/mirror-layout.sh --version v0.3.3` (upload the contents of `dist-mirror/` to the site root):

| URL | What it is |
| --- | --- |
| `https://deeprouter.top/install.sh` | installer, mirror host by default |
| `https://deeprouter.top/install.ps1` | Windows installer, mirror host by default |
| `https://deeprouter.top/releases/latest.txt` | one line, `v0.3.3` and a newline |
| `https://deeprouter.top/releases/latest/<asset>` | same bytes as the version directory |
| `https://deeprouter.top/releases/v0.3.3/<asset>` | `checksums.txt` and the six binaries |

`<asset>` is `checksums.txt`, `deeprouter-darwin-amd64`, `deeprouter-darwin-arm64`, `deeprouter-linux-amd64`, `deeprouter-linux-arm64`, `deeprouter-windows-amd64.exe`, or `deeprouter-windows-arm64.exe`. `latest/` is a real copy, not a symlink. The installer does not read `latest.txt`.

If npmjs.org is slow, the wizard mentions `npm config set registry https://registry.npmmirror.com` and can pass `--registry https://registry.npmmirror.com` for that one install. It does not change npm's saved config. Run the `npm config set` command yourself if you want it for later installs.

## Quick start

```sh
deeprouter
```

Running it in a terminal with no arguments starts interactive setup. `install.sh` enters this step after install when a controlling terminal is present. Only the tools you select are changed.

1. Choose a platform. The prompt is `Platform [1]:`. `1` is DeepRouter (`https://deeprouter.top`, the default), `2` is WolfAI (`https://wolfai.top`), `3` is LinkMesh (`https://www.linkmesh.org`), and `4` asks for a custom base URL. Enter selects the highlighted default. `DEEPROUTER_PROVIDER=wolfai` (or `linkmesh`) moves that default. Chinese prompts are used only in a terminal when `LC_ALL` (if set) or else `LANG` starts with `zh`, including `zh_CN` and `zh_TW`. Flags and a non-terminal session stay in English.
2. Choose programs. More than one is allowed. The prompt is `Programs [1]:`. Enter is `1`, which configures only Codex / ChatGPT. Choose Claude Code, Claude Desktop, Grok Build, or Gemini CLI yourself. Codex CLI and the ChatGPT desktop app share `~/.codex/config.toml`, so the list has one entry for both. A selection with no configurable program asks again. When stdin is not a terminal, the no-argument wizard also configures only Codex / ChatGPT and asks for the gateway URL instead of the platform list.
3. Gateway URL, for a custom platform or a non-terminal run. The prompt is exactly `Gateway URL (Enter for https://deeprouter.top, or any compatible endpoint):`. Enter uses `https://deeprouter.top`, or the `DEEPROUTER_PROVIDER` preset when that variable is set and the line is empty. A typed URL overrides the preset URL and keeps the provider id. Any other compatible endpoint is accepted. A bad URL asks again. Presets `1`–`3` skip this question and use that platform's URL.
4. **Each selected program** gets its own token and model questions. The prompt names the platform, for example `Enter the WolfAI token (create one at wolfai.top) for Codex / ChatGPT: ` (Chinese: `请输入 WolfAI 的令牌（在 wolfai.top 创建），用于 Codex / ChatGPT：`). A key saved for another platform is not offered. An existing key for this same platform looks like `Enter the WolfAI token (create one at wolfai.top) for Codex / ChatGPT [sk-n…Mc04]: `. Each typed character echoes as `*`. After submit the wizard prints `Entered (sk-****abcd, 51 chars)`. Enter keeps the current key and prints `Kept (sk-****abcd, 51 chars)`. Model option 1 is that program's own default: Enter writes no model field and removes a model this tool wrote earlier. The other numbers are chat model ids from the gateway for that family (Codex still wants an id containing `gpt`, Claude `claude`, Gemini `gemini`, Grok `grok`). Image, video, audio, embedding, speech, and moderation models are omitted, including ids containing image, nano-banana, dall-e, sora, veo, imagen, embedding, tts, whisper, audio, realtime, moderation, flare, or sunburst. Type or capability fields on the model object win over the id. If a family is filtered down to nothing, the picker falls back to that family's unfiltered list. One key fetches the model list once.
5. A selected program that is not installed gets a notice and the official install command before configuration, and the configuration is still written. `Next:` prints the notice again, in addition to `Then run:`. Lookup checks `PATH` and common npm global and Homebrew directories (`/opt/homebrew/bin`, `/usr/local/bin`, `~/.npm-global/bin`, and similar). Claude Code is `curl -fsSL https://claude.ai/install.sh | bash` (Windows: `irm https://claude.ai/install.ps1 | iex`). Codex / ChatGPT is `npm install -g @openai/codex`. Grok Build is `curl -fsSL https://x.ai/cli/install.sh | bash` (Windows: `irm https://x.ai/cli/install.ps1 | iex`). Claude Desktop is https://claude.com/download. Gemini CLI is `npm install -g @google/gemini-cli`, and macOS and Linux also have `brew install gemini-cli`.
6. Selected program configs are written, then a connectivity test runs. The terminal wizard does not edit the shell startup file. GUI apps on macOS do not read `~/.zshrc`.
7. If `claude` or `codex` is missing, the wizard asks `Install missing CLIs with npm? [y/N]` at the end. Anything other than `y` installs nothing. When npmjs.org does not answer quickly, or the locale or time zone looks like mainland China, it first mentions `npm config set registry https://registry.npmmirror.com` and asks whether to pass `--registry https://registry.npmmirror.com` for this install only. That does not change npm's saved config.

Scripts, and any run whose stdin is not a terminal, use `setup` and flags:

```sh
export DEEPROUTER_API_KEY=sk-your-key
deeprouter setup
deeprouter setup --provider wolfai
deeprouter setup --provider linkmesh --base-url https://example.com
deeprouter setup --apps grok --grok-model grok-4 --grok-key "$GROK_KEY"
deeprouter setup --claude-model default --codex-model default
deeprouter test
```

Each preset has its own token. The prompt names the site, for example `Enter the WolfAI token (create one at wolfai.top): ` and `请输入 WolfAI 的令牌（在 wolfai.top 创建）`. A key saved for another provider is not reused. `DEEPROUTER_API_KEY` is the DeepRouter token only. LinkMesh is a separate gateway: connectivity checks call `/v1/models` and the chat routes, not new-api `/api/status`.

LinkMesh (`https://www.linkmesh.org`) runs sub2api. Without a key, `POST /v1/chat/completions`, `POST /v1/responses`, and `POST /v1/messages` answer 401 `API_KEY_REQUIRED`, and `POST /v1beta/models/{model}:generateContent` answers 401 in Gemini's error shape. `POST /v1/models/{model}:generateContent` is 404, so Gemini is pointed at the origin and the CLI calls `/v1beta`. Codex uses `wire_api = "responses"` and `base_url` `https://www.linkmesh.org/v1`. Claude Code and Claude Desktop use `https://www.linkmesh.org` with no `/v1`. Grok Build uses the chat-completions route. All five programs are written. A program whose route is missing is skipped, with a message such as `LinkMesh does not expose POST /v1/messages, so Claude Code is skipped.`

`setup` without `--apps` still writes Claude Code, Claude Desktop, Codex / ChatGPT, and the shell startup file, so existing scripts keep working. `--apps` writes only the listed programs and leaves the shell alone. `--claude-only` and `--codex-only` cannot be combined with `--apps`.

## Commands

| Command | What it does |
| --- | --- |
| `setup` | Write the selected programs for one profile |
| `test` | `GET /v1/models`, one single-token chat completion, and one single-token messages call. After Gemini CLI is configured, also `POST /v1beta/models/{model}:generateContent` with the `x-goog-api-key` header (the selected id, or a Gemini chat id from the list when none was selected). Image, video, audio, embedding, speech, and moderation ids are not used as the default; the unfiltered list is the fallback when the chat filter leaves nothing. Prints HTTP status, time to first byte, and total time |
| `models` | List model ids on the gateway |
| `switch` | Switch profile, or change one program's model |
| `profile list\|add\|use\|remove` | Named profiles. One profile stores one API key |
| `status` | Active profile, which tools are wired, and whether `claude` and `codex` are installed (with versions) |
| `restore` | Put changed files back from a backup |
| `uninstall` | Restore those files. `--purge` also deletes `~/.deeprouter` |
| `version` | Print the version |

`deeprouter <command> --help` lists flags. `test` and `models` accept `--json`.

Exit codes: `0` success, `1` the gateway check or a file write failed, `2` the command line is wrong.

## Non-interactive flags

```sh
deeprouter setup \
  --base-url https://deeprouter.top \
  --apps claude-code,claude-desktop,codex,grok \
  --api-key "$DEEPROUTER_API_KEY" \
  --claude-model claude-sonnet-5-5 \
  --claude-desktop-model claude-opus-5-5 \
  --codex-model gpt-6.1-sol \
  --grok-model grok-4 \
  --claude-code-key "$CLAUDE_KEY" \
  --claude-desktop-key "$DESKTOP_KEY" \
  --codex-key "$CODEX_KEY" \
  --grok-key "$GROK_KEY"
```

| Flag | What it does |
| --- | --- |
| `--provider NAME` | `deeprouter` (default, `https://deeprouter.top`), `wolfai` (`https://wolfai.top`), or `linkmesh` (`https://www.linkmesh.org`). `DEEPROUTER_PROVIDER` selects the same preset. `--base-url` overrides the URL and keeps this id. Codex writes `model_provider = "<id>"` and `[model_providers.<id>]`. Grok Build writes `[model.<id>]`. Configuring again removes the previous managed section (`deeprouter`, `wolfai`, `linkmesh`, or `custom`) and leaves unrelated tables alone. `install.sh` and `install.ps1` pass `--provider` and `DEEPROUTER_PROVIDER` through to setup |
| `--apps LIST` | `claude-code`, `claude-desktop`, `codex`, `grok`, `gemini`, separated by commas or spaces |
| `--api-key` / `--api-key-stdin` / `DEEPROUTER_API_KEY` | Shared key. Used for any program that has no key of its own |
| `--claude-code-key`, `--claude-desktop-key`, `--codex-key`, `--grok-key`, `--gemini-key` | Written only to that program. Omitted flags use the shared key |
| `--claude-model`, `--claude-desktop-model`, `--codex-model`, `--grok-model`, `--gemini-model` | Model id for that program. `default` omits the model field and removes a model this tool wrote earlier, so the program uses its own default. When the flag is omitted, a chat model is picked from that provider's `GET /v1/models` (non-chat ids are skipped; an empty chat list falls back to the unfiltered family). The same family order is used for every preset: Claude prefers sonnet, then opus, then fable; Codex prefers an id containing `gpt-5`, then `gpt-4o`, then another `gpt` id. A provider whose list lacks the usual id gets the next match from its own list. Grok requires `--grok-model` or `--grok-model default` when the list has no id containing `grok`. Gemini leaves `model.name` unset when the list has no id containing `gemini` |
| `--no-shell` | Leave the Claude / Codex environment variables alone. When Gemini CLI is selected, `GEMINI_API_KEY` and `GOOGLE_GEMINI_BASE_URL` are still written to the same block, because Gemini CLI reads them from the environment |
| `--claude-only` | Write Claude Code and Claude Desktop, and skip Codex / ChatGPT. Cannot be combined with `--apps` |
| `--codex-only` | Write only Codex / ChatGPT, and skip Claude Code and Claude Desktop |

`--api-key` shows up in the process list. Scripts should use `DEEPROUTER_API_KEY` or `--api-key-stdin`.

`switch` updates only the programs that were actually written last time. A profile that only configured Grok Build does not touch Claude Code or Codex / ChatGPT on switch. Models can be changed on their own with `--claude-model`, `--claude-desktop-model`, `--codex-model`, `--grok-model`, and `--gemini-model`.

## Profiles

A profile stores the gateway URL, the API key, and optional model ids. The name is yours. There is no official-key group.

```sh
deeprouter setup --api-key "$DEEPROUTER_API_KEY"
deeprouter profile add azure --label Azure --api-key "$AZURE_KEY"
deeprouter switch --profile azure
```

Switching profiles rewrites the files configured last time. Running again replaces them in place. It does not append a second block to `~/.zshrc`, and it does not write the same key twice.

## Where each program is written

The model ids below are examples. Use an id your gateway actually lists. `--base-url` may include `/v1` or not: Claude receives the URL without a trailing `/v1` (the client requests `/v1/messages` itself), and Codex / ChatGPT and Grok Build receive the URL with `/v1`.

### Claude Code

`~/.claude/settings.json` (on Windows, `%USERPROFILE%\.claude\settings.json`). Other keys are kept. File mode `0600`. Environment variables here take priority over the shell.

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://deeprouter.top",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-key",
    "ANTHROPIC_MODEL": "claude-sonnet-5-5"
  }
}
```

The Code tab inside Claude Desktop does not use this file for the gateway.

### Claude Desktop (Chat, Cowork, and Code tabs)

Claude Desktop is one app. Claude Code inside it is the Code tab, not a second desktop program. In the official docs, desktop gateway routing comes from third-party inference configuration. It does not read `ANTHROPIC_BASE_URL` or `settings.json`. Chat, Cowork, and Code share that configuration. The gateway fields are passed into every Code session and cannot be overridden by `~/.claude/settings.json`.

With no managed configuration, the app stores local config under `configLibrary/` in the third-party app data directory ([User identity and local data](https://claude.com/docs/third-party/claude-desktop/data-storage)):

| Platform | Directory |
| --- | --- |
| macOS | `~/Library/Application Support/Claude-3p/configLibrary/` |
| Windows | `%APPDATA%\Claude\configLibrary/` |
| Linux | `~/.config/Claude-3p/configLibrary/` |

Windows uses the same `configLibrary` layout as macOS Claude-3p (`_meta.json` plus one `.json` per config). The directory is under `%APPDATA%\Claude`, the roaming location that corresponds to Application Support. An empty `%APPDATA%` falls back to `%USERPROFILE%\AppData\Roaming\Claude\configLibrary\`. Linux follows the docs and uses `~/.config`, not `$XDG_CONFIG_HOME`.

`_meta.json` records which config is active. Each config is a `.json` file beside it. The public docs do not name the `_meta.json` fields. This tool uses the shape the in-app configuration window actually writes: `appliedId` and `entries` (`id`, `name`). An already-applied config is updated in place, and the selection stays. When none is applied, a config named DeepRouter is created and selected. Other saved configs stay in the list. The API key is not written to `_meta.json`.

The config file uses the official gateway fields ([LLM gateway](https://claude.com/docs/third-party/claude-desktop/gateway)):

```json
{
  "inferenceProvider": "gateway",
  "inferenceGatewayBaseUrl": "https://deeprouter.top",
  "inferenceGatewayApiKey": "sk-your-key",
  "inferenceCredentialKind": "static",
  "inferenceGatewayAuthScheme": "bearer",
  "inferenceModels": [{ "name": "claude-sonnet-5-5" }]
}
```

`inferenceGatewayBaseUrl` has no trailing `/v1`. The in-app example has this shape (`https://llm-gateway.example.corp`). Code itself requests `POST /v1/messages`. `GET /v1/models` is optional. A single model id is written to `inferenceModels`, unless the file already lists more than one model. With no model list, `modelDiscoveryEnabled` is set to true only when that field is absent. Discovery shows only Claude model ids the app recognizes.

Both files are mode `0600`. Managed configuration overrides this directory: a macOS configuration profile, Windows machine policy, or the Linux file `/etc/claude-desktop/managed-settings.json`, which must be owned by root. This tool does not write those locations. A bad `/etc` file disables the local settings, and it has to belong to root.

Setting `inferenceProvider` puts the app in third-party mode. Unless Claude.ai was already chosen at login, the app opens in that mode. This tool does not set `disableDeploymentModeChooser`, so a Claude.ai login stays available. If Claude.ai was selected before, open Developer Mode and use Developer → Configure Third-Party Inference, or skip the Anthropic login, then quit and reopen the app.

### Codex / ChatGPT

Codex CLI and the ChatGPT desktop app share `~/.codex/config.toml`. `CODEX_HOME` changes that directory. The Windows default is `%USERPROFILE%\.codex\config.toml`. In the app: Settings → Configuration → Open config.toml. The picker has one Codex / ChatGPT entry, and writing it once covers both. IDE extensions read this file too.

```toml
model = "gpt-6.1-sol"
model_provider = "deeprouter"

[model_providers.deeprouter]
name = "Officially"
base_url = "https://deeprouter.top/v1"
experimental_bearer_token = "sk-your-key"
wire_api = "responses"
requires_openai_auth = false
```

`model` and `model_provider` stay in the root table, before the first `[table]`, which is where Codex reads them. Comments and unrelated tables are kept. File mode `0600`.

The id follows the provider. `--provider wolfai` writes `model_provider = "wolfai"` and `[model_providers.wolfai]` with `base_url = "https://wolfai.top/v1"`. LinkMesh uses `linkmesh` and `https://www.linkmesh.org/v1`. A custom wizard URL uses `custom`. The next configure removes the previous managed section and does not touch `[model_providers.openai]` or other tables.

Codex removed `disable_response_storage` (requests are never stored). This tool does not write that key. The next Codex / ChatGPT configure removes it from the root table and from the managed provider section. The same key in any other table is left as it is.

The key is `experimental_bearer_token`. The app server and processes started from the GUI read `config.toml` directly and do not need an environment variable. `env_key` is not written: if it is present, Codex returns `Missing environment variable: DEEPROUTER_API_KEY` before it looks at the bearer token.

`~/.codex/auth.json` (or the system keychain when `cli_auth_credentials_store` is `keyring` or `auto`) is the ChatGPT / OpenAI API key login cache, not credentials for a custom provider. This tool does not write the gateway key there.

Codex / ChatGPT uses the Responses API, so `wire_api` defaults to `responses`. Current Codex accepts only `responses` and rejects `wire_api = "chat"`. Older builds can still pass `--codex-wire-api chat`. The wizard's connectivity test calls only the endpoints for the programs selected in that run. Codex / ChatGPT checks `GET /v1/models` and `POST /v1/chat/completions`. Claude Code and Claude Desktop check `POST /v1/messages`. Grok Build checks `POST /v1/chat/completions`. Gemini CLI checks `POST /v1beta/models/{model}:generateContent` with `x-goog-api-key`. When the model was left as the app default, the test picks a chat model of that family from the gateway list, and skips an endpoint the list cannot serve. `deeprouter test` checks the programs saved on the active profile.

### Grok Build

xAI's coding CLI. The command is `grok`. User config is `~/.grok/config.toml` (`$GROK_HOME/config.toml`; on Windows, `%USERPROFILE%\.grok\config.toml`). Docs: [Settings](https://docs.x.ai/build/settings), [Settings reference](https://docs.x.ai/build/settings/reference), [Overview](https://docs.x.ai/build/overview).

A custom model is a `[model.<id>]` table. `base_url` is OpenAI-style and includes `/v1`. `api_backend` can be `chat_completions`, `responses`, or `messages`. This tool writes `chat_completions` because the connectivity test and the gateway convention use `POST /v1/chat/completions`.

```toml
[models]
default = "deeprouter"

[model.deeprouter]
model = "grok-4"
base_url = "https://deeprouter.top/v1"
name = "Officially"
api_key = "sk-your-key"
api_backend = "chat_completions"
```

`--provider wolfai` uses `default = "wolfai"` and `[model.wolfai]` instead. The previous managed `[model.<id>]` is removed. Other `[model.*]` tables stay.

Credential order is `api_key` > `env_key` > a signed-in session > `XAI_API_KEY`. The docs prefer `env_key` so the key stays out of the file. This tool still writes `api_key` into the file and removes `env_key` from the managed `[model.<id>]`, so a launch from the Dock or anywhere else without the shell environment can read the key. File mode `0600`. The section id is the provider id (`deeprouter` unless `--provider` or `DEEPROUTER_PROVIDER` says otherwise). Changing models only edits the `model` field. Other `[model.*]` tables, `[mcp_servers.*]`, comments, and `[models].web_search` are kept.

A project `.grok/config.toml` only contributes MCP, plugins, and permission rules. The full user config is read from `~/.grok/config.toml`. This tool does not write the project file, `managed_config.toml`, `requirements.toml`, or MDM. Those layers have higher priority and override the user file.

`GROK_MODELS_BASE_URL` with `XAI_API_KEY` can also discover models through the environment. A GUI or non-shell launch does not see the shell environment, so that is not how this tool writes the config. Start a new Grok Build session after a change so it reads `config.toml` again.

### Gemini CLI

Gemini CLI uses Gemini's `generateContent` API. `@google/genai` treats `GOOGLE_GEMINI_BASE_URL` as the base and appends the API version and path. The default request is `{base}/v1beta/models/{model}:generateContent`. The value written here is the gateway origin, for example `https://deeprouter.top`, with no `/v1` and no `/v1beta`. Auth is `GEMINI_API_KEY` (the `x-goog-api-key` header). Gemini CLI requires HTTPS for this base. Only `localhost`, `127.0.0.1`, and `[::1]` may use HTTP.

Those two variables are written in the same place as the other environment variables: the marked block in the shell startup file on macOS and Linux, and the user environment `HKCU\Environment` on Windows. `restore` and `uninstall` put them back to the values from before the first write. `--apps gemini` writes them too. `--no-shell` does not skip them.

User settings are `~/.gemini/settings.json` (`GEMINI_CLI_HOME` changes the parent of `.gemini`). The Windows default is `%USERPROFILE%\.gemini\settings.json`. This tool merges two fields and leaves the rest of the file alone:

- `security.auth.selectedType` is set to `gemini-api-key`, so startup does not ask for a Google login.
- A selected model is written to `model.name`. Model option 1, Enter, or `--gemini-model default` deletes `model.name` so Gemini CLI uses its own default. Other fields under `model` stay.

When it is not installed, the wizard and `setup` print the official command `npm install -g @google/gemini-cli`. Docs: https://geminicli.com/docs/get-started/installation/. The configuration is still written. A project `.gemini/settings.json` overrides user settings. System settings are `/etc/gemini-cli/settings.json` on Linux, `C:\ProgramData\gemini-cli\settings.json` on Windows, and `/Library/Application Support/GeminiCli/settings.json` on macOS. Docs: [configuration](https://geminicli.com/docs/reference/configuration/).

### Shell startup file

`setup` without `--apps`, and the no-argument wizard when stdin is not a terminal, write a marked export block on macOS and Linux in `~/.zshrc`, `~/.bashrc`, or `~/.config/fish/config.fish` so an already-open terminal can `source` it. The terminal wizard and `--apps` do not write that block for Claude Code or Codex / ChatGPT. Gemini CLI is the exception: `GEMINI_API_KEY` and `GOOGLE_GEMINI_BASE_URL` go in the same block, because Gemini CLI reads the key and base URL from the environment. Windows does not edit `.zshrc` or a PowerShell profile. The same variables are written to the current user's environment (registry `HKCU\Environment`, the same effect as `setx` without the 1024-character truncation). A newly started process inherits them. `restore` and `uninstall` put those variables back to the values from before the first write. With `--rc-file`, Windows edits that startup file and does not write user environment variables. Codex / ChatGPT, Claude Desktop, and Grok Build do not depend on this export.

```sh
# >>> deeprouter-setup >>>
export ANTHROPIC_BASE_URL="https://deeprouter.top"
export ANTHROPIC_AUTH_TOKEN="sk-your-key"
export ANTHROPIC_MODEL="claude-sonnet-5-5"
export DEEPROUTER_API_KEY="sk-your-key"
# <<< deeprouter-setup <<<
```

Running again replaces that block. It does not append a second copy.

## What stays on the vendor

A normal Claude.ai chat does not use a custom gateway. Claude Code in Slack, and Claude Code cloud sessions, are outside a gateway deployment: gateway variables in a cloud session have no effect. Remote Control is unavailable while the desktop gateway configuration is active. Those surfaces keep using Anthropic.

Managed policy wins over local files: Claude Desktop MDM / `/etc/claude-desktop/managed-settings.json`, and Grok Build `managed_config.toml`, `requirements.toml`, and MDM. This tool does not write those locations.

## FAQ

### Where does the API key go?

A full `setup` without `--apps` writes the key to:

- `~/.deeprouter/config.json`, mode `0600`. When programs use different keys, the profile stores the overrides as well
- `env.ANTHROPIC_AUTH_TOKEN` in `~/.claude/settings.json`. The Claude Code CLI reads it. The Code tab in Claude Desktop does not use it as the gateway credential
- `experimental_bearer_token` under `[model_providers.deeprouter]` in `~/.codex/config.toml`. The Codex / ChatGPT CLI, desktop app, and IDE extension read it. After the write, the value is checked against the profile, and the provider must not have `env_key`. `auth.json` is left alone
- `inferenceGatewayApiKey` in the active file under the Claude Desktop `configLibrary/`
- `ANTHROPIC_AUTH_TOKEN` and `DEEPROUTER_API_KEY` in the shell startup file. On Windows those two live in the user environment, not in a startup file

A partial selection writes the key only into those programs' files and the profile. `GET /v1/models` and the chat completion use `Authorization: Bearer`. The messages check also sends `x-api-key` and `anthropic-version`. Redirects are not followed.

`deeprouter status` prints whether `claude` and `codex` are on `PATH`, and the first line of each `--version`. After the wizard, the installed or not-installed lines and the `Install missing CLIs with npm? [y/N]` prompt cover only the programs selected in that run. Codex / ChatGPT notes that the desktop app uses the same config, so answering N is fine when only the desktop app is used. Claude Desktop has no npm package. Grok Build prints its own installer instead of joining the npm prompt. `setup --install-clis` runs `npm install -g` for the missing npm CLIs among the programs being configured (`@anthropic-ai/claude-code`, `@openai/codex`, `@google/gemini-cli`). With no `npm`, it prints the official install pages and installs nothing.

When a key already exists for the same platform, the interactive prompt names that site, for example `Enter the WolfAI token (create one at wolfai.top) [sk-n…Mc04]:`. Enter keeps it. A key saved for a different platform is not reused. `DEEPROUTER_API_KEY` is the DeepRouter token only. When stdin is not a terminal there is no prompt: the run uses `--api-key`, that DeepRouter variable when the provider is DeepRouter, or the key already saved for this provider. LinkMesh is not called via `/api/status`.

`~/.deeprouter/backups` holds a copy of each file from before a change, so it can contain a key that used to live in `settings.json` or a shell rc. `deeprouter uninstall --purge` deletes that directory.

### How do I undo this?

`deeprouter restore` writes back the copy from before this tool first changed each file. Files this tool created are deleted. Profiles stay, so you can run `setup` again.

`deeprouter restore --list` lists later snapshots. `deeprouter restore --id latest` undoes only the most recent change.

`deeprouter uninstall` does the same restore. `--purge` deletes `~/.deeprouter`, including saved profiles and backups.

### Can I use another gateway?

Yes. Any base URL that serves `GET /v1/models`, `POST /v1/chat/completions`, and `POST /v1/messages` works. When Gemini CLI is configured, that same base also needs `POST /v1beta/models/{model}:generateContent`.

```sh
deeprouter setup --base-url https://gateway.example.com --api-key "$GATEWAY_KEY"
```

`test` reports the HTTP status and a short hint: `401` bad key, `403` quota or permission, `404` model or path, `503` no route available.

### Does Windows work?

Yes. Releases include `deeprouter-windows-amd64.exe` and `deeprouter-windows-arm64.exe`, with checksums in `checksums.txt`. Install with the PowerShell or cmd command above. Interactive questions use the Windows console: each API key character is shown as `*` (the console reads character by character; there is no `stty` and no `/dev/tty`).

Claude Code is written to `%USERPROFILE%\.claude\settings.json`. Codex / ChatGPT is written to `%USERPROFILE%\.codex\config.toml` (`CODEX_HOME` can change the directory). Claude Desktop third-party config is written to `%APPDATA%\Claude\configLibrary\`. Grok Build is written to `%USERPROFILE%\.grok\config.toml` (`GROK_HOME` can change the directory). Environment variables go to the user environment `HKCU\Environment`, not a shell startup file.

## Why Go

The tool is one static binary. The module has no third-party dependencies. Editing someone else's JSON, TOML, and shell startup file has to be idempotent and has to leave the rest of the file in place, which is easier to test in Go than in a shell script alone. `install.sh` and `install.ps1` are two single-file entry points: they compile that binary in a checkout, or download the release when you run them from the network. macOS and Linux use `install.sh`. Windows uses `install.ps1`, and user environment variables are written to the registry rather than a shell startup file.

## Development

```sh
go test ./...
go build -o deeprouter .
```

CI runs tests on Linux, macOS, and Windows, runs `install.sh` on Linux and macOS, runs `install.ps1` on Windows, and cross-compiles `windows/amd64` and `windows/arm64` on Linux. The core coverage is config writes (Claude settings, Codex TOML, Claude Desktop `configLibrary`, Grok Build `config.toml`, shell rc, Windows user environment variables, backups) and an end-to-end install, setup, test, switch, and restore flow against a local fake gateway.

Pushing a `v*` tag runs `.github/workflows/release.yml`. The workflow builds static binaries with `CGO_ENABLED=0` for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64, and windows/arm64. The Windows artifacts are `.exe` files, plain files like the other platforms, not zips. SHA256 sums go in `checksums.txt`, and a GitHub Release is published. The version inside the binary is the tag with the leading `v` removed (`v0.1.0` becomes `0.1.0`), injected with `-X main.version`.

## License

MIT. See [LICENSE](LICENSE).

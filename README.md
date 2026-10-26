# deeprouter-setup

One command to point Claude Code and Codex CLI at any OpenAI- and Anthropic-compatible API gateway.

![deeprouter-setup configuring Claude Code and Codex](docs/demo.gif)

Replace `docs/demo.gif` with a short screen recording before the Product Hunt and Show HN launch.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.sh | sh
```

The binary is installed to `~/.local/bin`. If that directory is not on `PATH`, the installer prints the `export` line and adds it to your shell startup file (`~/.zshrc`, `~/.bashrc`, or the fish config). Pass `--no-path` to skip the startup-file edit. Open a new terminal, then run:

```sh
deeprouter-setup
```

From a checkout:

```sh
git clone https://github.com/zfg5118/deeprouter-setup.git
cd deeprouter-setup
sh install.sh
```

`install.sh` is POSIX `sh`. Next to this repo it builds a static binary with Go 1.22 or newer. The version embedded in that binary is `git describe --tags`, or `dev` when the checkout has no tags. Piped from the web, it downloads a plain release binary (not an archive) from `https://github.com/zfg5118/deeprouter-setup/releases/latest/download/` and checks it against `checksums.txt` at that same path. That does not call the GitHub API. The asset names are `deeprouter-setup-darwin-amd64`, `deeprouter-setup-darwin-arm64`, `deeprouter-setup-linux-amd64`, and `deeprouter-setup-linux-arm64`. Set `DEEPROUTER_SETUP_VERSION` without the leading `v` (for example `0.1.1`) to pin `releases/download/v0.1.1/` instead of latest, and `DEEPROUTER_SETUP_REPO=owner/name` if you fork the repository. There is no runtime dependency after install.

## Quick start

```sh
deeprouter-setup
```

With no arguments, in a terminal, that starts setup:

1. It asks for an API key. Input is hidden. If `DEEPROUTER_API_KEY` is already set, or a key is already saved in the profile, Claude Code settings, Codex config, or the shell startup file, the prompt shows that key masked, for example `API key [sk-n…Mc04]:`. Press Enter to keep it, or type a new one to replace it. The replacement is written to every file setup manages. When stdin is not a terminal, that key is used and the prompt is skipped.
2. It asks for an official-key group. Press Enter to keep Officially.
3. It fetches the live model list and asks you to confirm a Claude model (ids containing `claude`) and a Codex model (ids containing `gpt`). Press Enter to accept the suggested id.
4. It writes Claude Code, Codex, shell, and Claude Desktop configuration, runs the connectivity test, and prints a one-liner for the current shell (`source ~/.zshrc` on zsh). Codex reads its key from `~/.codex/config.toml`, so the CLI, the desktop app, and the IDE extension work without that line. Claude Desktop reads its own local third-party configuration, also written here, so it does not need the shell exports either. The line still loads those exports for an already-open terminal. Quit and reopen the desktop apps after setup.
5. If `claude` or `codex` is missing, it asks before installing them (`y/N`). Pass `--install-clis` on the non-interactive command to install without a prompt. Nothing is installed unless you answer `y` or pass that flag.

The default gateway is `https://deeprouter.top`. Press Enter at the gateway prompt to keep it, or pass `--base-url` on the non-interactive command.

Flags still work, and `setup` does not prompt. It reads `--api-key`, `--api-key-stdin`, `DEEPROUTER_API_KEY`, or the key already saved on the profile. When `--claude-model` or `--codex-model` is omitted, the model is chosen from the gateway list:

```sh
export DEEPROUTER_API_KEY=sk-your-key
deeprouter-setup setup --group officially
deeprouter-setup test
```

## Commands

| Command | What it does |
| --- | --- |
| `setup` | Write Claude Code, Codex, Claude Desktop, and shell configuration for one profile |
| `test` | `GET /v1/models`, a one-token chat completion, and a one-token messages call. Prints HTTP status, time to first byte, and total time |
| `models` | List model ids from the gateway |
| `switch` | Change the active profile, the Claude model, or the Codex model |
| `profile list\|add\|use\|remove` | Manage named profiles. One profile holds one API key |
| `groups` | Show the recommended official-key groups |
| `status` | Show the active profile, whether each tool is wired, and whether `claude` and `codex` are installed (with version) |
| `restore` | Put edited files back from a backup |
| `uninstall` | Restore those files. `--purge` also deletes `~/.deeprouter` |
| `version` | Print the version |

Run `deeprouter-setup <command> --help` for flags. `test` and `models` accept `--json`.

Exit codes: `0` success, `1` the gateway check or a file write failed, `2` the command line was wrong.

## Groups and profiles

A group is selected by the API key you store on a profile. Official-key groups to start from:

| Profile | Label | Use for |
| --- | --- | --- |
| `openai` | OpenAI | Official OpenAI keys |
| `azure` | Azure | Azure OpenAI keys |
| `aws` | AWS | Amazon Bedrock keys |
| `vertex` | Vertex | Google Vertex keys |
| `officially` | Officially | Official provider keys |

```sh
deeprouter-setup setup --group officially --api-key "$DEEPROUTER_API_KEY"
deeprouter-setup profile add azure --label Azure --api-key "$AZURE_KEY"
deeprouter-setup switch --group azure
```

Switching a profile rewrites the same managed files: the shell block, `~/.claude/settings.json`, the `deeprouter` provider in `~/.codex/config.toml`, and the applied Claude Desktop configuration.

## What gets written

The model ids below are whatever you confirmed from the gateway list.

Claude Code, in `~/.claude/settings.json` (other keys are kept):

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://deeprouter.top",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-key",
    "ANTHROPIC_MODEL": "claude-sonnet-4-6"
  }
}
```

The same variables are exported from a marked block in your shell startup file (`~/.zshrc`, `~/.bashrc`, `~/.config/fish/config.fish`, or the PowerShell profile). Codex does not depend on that export. The key is stored on the provider as `experimental_bearer_token`, which the Codex app-server and GUI launches read from `config.toml` directly. `env_key` is omitted: when it is set, Codex returns `Missing environment variable: DEEPROUTER_API_KEY` before it considers the bearer token.

```toml
model = "gpt-5.4"
model_provider = "deeprouter"

[model_providers.deeprouter]
name = "Officially"
base_url = "https://deeprouter.top/v1"
experimental_bearer_token = "sk-your-key"
wire_api = "responses"
requires_openai_auth = false
```

`model` and `model_provider` stay in the root table, before the first `[table]`, which is where Codex reads them. Comments and unrelated tables are left in place. A URL with or without a trailing `/v1` is accepted: Claude Code is given the base without `/v1` (it calls `/v1/messages` itself) and Codex is given the base with `/v1`.

Running `setup` again replaces that managed block and those keys. A new API key replaces the old one in the profile, Claude Code settings, Codex config, the Claude Desktop configuration, and the shell startup file. It does not append a second copy or a second `DEEPROUTER_API_KEY` line.

## Desktop apps

A macOS GUI app does not load `~/.zshrc`. A key that exists only as `DEEPROUTER_API_KEY` or `ANTHROPIC_AUTH_TOKEN` in the shell startup file never reaches Codex or Claude Desktop. Setup writes the key and the base URL into the files those apps read. Quit the app and open it again after a write. `--codex-only` skips Claude Desktop. `--claude-only` skips Codex.

### Codex desktop app

The Codex app, the CLI, and the IDE extension share `~/.codex/config.toml`. `CODEX_HOME` overrides that directory. On Windows the default is `%USERPROFILE%\.codex\config.toml`. In the app: Settings → Configuration → Open config.toml. Setup writes `base_url` with `/v1` and `experimental_bearer_token` in that file, and omits `env_key`, so the app does not need `DEEPROUTER_API_KEY` in its environment.

`~/.codex/auth.json` (or the OS keychain, when `cli_auth_credentials_store` is `keyring` or `auto`) is the ChatGPT or OpenAI API-key login cache. It is not the custom-provider credential. Setup does not write the gateway key there.

### Claude Code in a terminal

`~/.claude/settings.json` holds `ANTHROPIC_BASE_URL` without a trailing `/v1` and `ANTHROPIC_AUTH_TOKEN` under `env`. On Windows that file is `%USERPROFILE%\.claude\settings.json`. The CLI reads it without a shell. Values in that file win over the shell.

That file is not what the Code tab inside Claude Desktop uses for the gateway.

### Claude Desktop (Chat, Cowork, and the Code tab)

Claude Desktop is one app. Claude Code there is the Code tab, not a separate desktop app. Official docs: the desktop app reads gateway routing from its third-party inference configuration, not from `ANTHROPIC_BASE_URL` or `settings.json`. Chat, Cowork, and Code share that configuration. The gateway keys are passed into every Code session and cannot be overridden by `~/.claude/settings.json`.

When no managed profile is present, the app stores locally authored configuration in `configLibrary/` under its third-party application-data directory ([User identity and local data](https://claude.com/docs/third-party/claude-desktop/data-storage)):

| Platform | Directory |
| --- | --- |
| macOS | `~/Library/Application Support/Claude-3p/configLibrary/` |
| Windows | `%LOCALAPPDATA%\Claude-3p\configLibrary/` |
| Linux | `~/.config/Claude-3p/configLibrary/` |

On Windows, older releases used `%APPDATA%\Claude-3p\`. Current releases move that directory to `%LOCALAPPDATA%` on upgrade. Setup writes the current path. Linux uses `~/.config`, which is the path in the docs, not `$XDG_CONFIG_HOME`.

`_meta.json` records which saved configuration is applied. Each configuration is a `.json` file beside it. The public docs do not publish the `_meta.json` field names. Setup uses `appliedId` and `entries` (`id`, `name`), which is the shape the in-app configuration window writes. If a configuration is already applied, setup updates that file and leaves the selection alone. If none is applied, it creates one named DeepRouter and selects it. Other saved configurations stay listed. The API key is not written into `_meta.json`.

The configuration file receives the official gateway keys ([LLM gateway](https://claude.com/docs/third-party/claude-desktop/gateway)):

```json
{
  "inferenceProvider": "gateway",
  "inferenceGatewayBaseUrl": "https://deeprouter.top",
  "inferenceGatewayApiKey": "sk-your-key",
  "inferenceCredentialKind": "static",
  "inferenceGatewayAuthScheme": "bearer",
  "inferenceModels": [{ "name": "claude-sonnet-4-6" }]
}
```

`inferenceGatewayBaseUrl` has no trailing `/v1`. The in-app field uses that shape (`https://llm-gateway.example.corp`). Code calls `POST /v1/messages` itself. `GET /v1/models` is optional. When setup has a single model id, it writes `inferenceModels` with that id, unless the file already lists more than one model. With no model list, it sets `modelDiscoveryEnabled` only when that key is absent. Discovery shows only model ids the app recognizes as Claude.

Both files are mode `0600`. A managed profile overrides this directory: a macOS configuration profile, Windows machine policy, or the root-owned Linux file `/etc/claude-desktop/managed-settings.json`. Setup does not write those. A bad `/etc` file can disable local settings, and it has to be owned by root.

Setting `inferenceProvider` asks the app for third-party mode. The app opens in that mode unless you already chose Claude.ai at the login screen. Setup does not set `disableDeploymentModeChooser`, so Claude.ai sign-in stays available. If you previously chose Claude.ai, pick third-party inference (Developer → Configure Third-Party Inference, after enabling Developer Mode) or skip Anthropic sign-in, then quit and reopen the app.

### What cannot use this gateway

A chat signed in to Claude.ai does not route through a custom gateway. Claude Code in Slack, and Claude Code cloud sessions, are not part of a gateway deployment: gateway variables in a cloud session are not applied. Remote Control is unavailable while the desktop gateway configuration is active. Those surfaces keep using Anthropic.

Current Codex speaks the Responses API, so `wire_api` defaults to `responses`. Pass `--codex-wire-api chat` for an older Codex build. The connectivity test always calls Chat Completions and Messages, which is the pair a compatible gateway publishes.

## FAQ

### Where do API keys go?

A full setup writes the key to five places:

- `~/.deeprouter/config.json`, mode `0600`, so profiles survive a switch
- `~/.claude/settings.json`, under `env.ANTHROPIC_AUTH_TOKEN` (the file is mode `0600` when this tool writes it). The Claude Code CLI reads this. The Code tab in Claude Desktop does not use it for the gateway.
- `~/.codex/config.toml`, as `experimental_bearer_token` under `[model_providers.deeprouter]`. The file is mode `0600`. The Codex CLI, desktop app, and IDE extension read it. After writing it, setup checks that this value matches the profile and that the provider has no `env_key`. `auth.json` is left alone.
- the applied file in Claude Desktop's `configLibrary/`, as `inferenceGatewayApiKey`, mode `0600`. See [Desktop apps](#desktop-apps).
- your shell startup file, as `ANTHROPIC_AUTH_TOKEN` and `DEEPROUTER_API_KEY`

The shell export stays so an already-open terminal can `source` its startup file. Codex itself uses the bearer token in `config.toml`. `GET /v1/models` and the chat completion send the key as `Authorization: Bearer`. The messages check also sends `x-api-key` and `anthropic-version`. Redirects are not followed.

`deeprouter-setup status` prints whether `claude` and `codex` are on `PATH` and the first line of each `--version`. The interactive setup asks `Install missing CLIs with npm? [y/N]` when one of them is missing. `setup --install-clis` runs `npm install -g @anthropic-ai/claude-code @openai/codex` for the ones that are missing. If `npm` is not on `PATH`, the command prints the official install pages instead of installing.

`--api-key` is visible to other users on the machine through the process list. Prefer `DEEPROUTER_API_KEY`, `--api-key-stdin`, or the interactive prompt (input is hidden when the terminal allows it). Press Enter at that prompt to keep the key that is already set.

Backups under `~/.deeprouter/backups` are copies of the files from before each edit, so they can contain a key that used to be in `settings.json` or your shell rc. `deeprouter-setup uninstall --purge` deletes that directory.

### How do I revert?

`deeprouter-setup restore` writes back the copy taken before this tool first changed each file. A file we created is removed. Profiles are kept, so you can `setup` again.

`deeprouter-setup restore --list` shows later snapshots. `deeprouter-setup restore --id latest` undoes only the most recent edit.

`deeprouter-setup uninstall` does the same restore. Add `--purge` to delete `~/.deeprouter`, including saved profiles and backups.

### Can I use another gateway?

Yes. Any base URL that serves `GET /v1/models`, `POST /v1/chat/completions`, and `POST /v1/messages` works.

```sh
deeprouter-setup setup --base-url https://gateway.example.com --api-key "$GATEWAY_KEY"
```

`test` reports the HTTP status and a short hint: `401` bad key, `403` quota or permission, `404` model or path, `503` no channel.

### Does this work on Windows?

The config writers run on Windows. Claude Code settings go to `%USERPROFILE%\.claude\settings.json`, Codex config to `%USERPROFILE%\.codex\config.toml`, Claude Desktop's local library to `%LOCALAPPDATA%\Claude-3p\configLibrary\`, and the shell block to the PowerShell profile. `install.sh` downloads release binaries for macOS and Linux. On Windows, build with `go build -o deeprouter-setup.exe .`.

## Why Go

The tool is one static binary and the module has no third-party dependencies. Editing someone else's JSON, TOML, and shell startup file has to be idempotent and has to keep the rest of the file, which is easier to test in Go than in a shell script alone. `install.sh` is the single-file entrypoint: it builds that binary from a checkout, or downloads a release when you pipe it from the web. macOS and Linux are required; Windows is the same Go code with a PowerShell profile instead of bash.

## Development

```sh
go test ./...
go build -o deeprouter-setup .
```

CI runs the tests on Linux, macOS, and Windows, and runs `install.sh` on Linux and macOS. The core coverage is the config writers (Claude settings, Codex TOML, Claude Desktop `configLibrary`, shell rc, backups) plus an end-to-end run of install, setup, test, switch, and restore against a local fake gateway.

Pushing a `v*` tag runs `.github/workflows/release.yml`. The workflow builds static binaries with `CGO_ENABLED=0` for darwin/amd64, darwin/arm64, linux/amd64, and linux/arm64, writes SHA256 sums to `checksums.txt`, and publishes a GitHub Release. The version embedded in each binary is the tag with the leading `v` removed (`v0.1.0` becomes `0.1.0`), via `-X main.version`.

## License

MIT. See [LICENSE](LICENSE).

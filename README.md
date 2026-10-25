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

1. It asks for an API key. Input is hidden. If `DEEPROUTER_API_KEY` is already set, that value is used.
2. It asks for an official-key group. Press Enter to keep Officially.
3. It fetches the live model list and asks you to confirm a Claude model (ids containing `claude`) and a Codex model (ids containing `gpt`). Press Enter to accept the suggested id.
4. It writes Claude Code, Codex, and shell configuration, runs the connectivity test, and prints the next step: open a new terminal, then run `claude` or `codex`.

The default gateway is `https://deeprouter.top`. Press Enter at the gateway prompt to keep it, or pass `--base-url` on the non-interactive command.

Flags still work. When `--claude-model` or `--codex-model` is omitted, the model is chosen from the gateway list:

```sh
export DEEPROUTER_API_KEY=sk-your-key
deeprouter-setup setup --group officially
deeprouter-setup test
```

## Commands

| Command | What it does |
| --- | --- |
| `setup` | Write Claude Code, Codex, and shell configuration for one profile |
| `test` | `GET /v1/models`, a one-token chat completion, and a one-token messages call. Prints HTTP status, time to first byte, and total time |
| `models` | List model ids from the gateway |
| `switch` | Change the active profile, the Claude model, or the Codex model |
| `profile list\|add\|use\|remove` | Manage named profiles. One profile holds one API key |
| `groups` | Show the recommended official-key groups |
| `status` | Show the active profile and whether each tool is wired |
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

Switching a profile rewrites the same managed files: the shell block, `~/.claude/settings.json`, and the `deeprouter` provider in `~/.codex/config.toml`.

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

The same variables are exported from a marked block in your shell startup file (`~/.zshrc`, `~/.bashrc`, `~/.config/fish/config.fish`, or the PowerShell profile). Codex reads the key from the environment, not from TOML:

```toml
model = "gpt-5.4"
model_provider = "deeprouter"

[model_providers.deeprouter]
name = "Officially"
base_url = "https://deeprouter.top/v1"
env_key = "DEEPROUTER_API_KEY"
wire_api = "responses"
requires_openai_auth = false
```

`model` and `model_provider` stay in the root table, before the first `[table]`, which is where Codex reads them. Comments and unrelated tables are left in place. A URL with or without a trailing `/v1` is accepted: Claude Code is given the base without `/v1` (it calls `/v1/messages` itself) and Codex is given the base with `/v1`.

Running `setup` again replaces that managed block and those keys. It does not append a second copy.

Current Codex speaks the Responses API, so `wire_api` defaults to `responses`. Pass `--codex-wire-api chat` for an older Codex build. The connectivity test always calls Chat Completions and Messages, which is the pair a compatible gateway publishes.

## FAQ

### Where do API keys go?

The key is written to three places:

- `~/.deeprouter/config.json`, mode `0600`, so profiles survive a switch
- `~/.claude/settings.json`, under `env.ANTHROPIC_AUTH_TOKEN` (the file is mode `0600` when this tool writes it)
- your shell startup file, as `ANTHROPIC_AUTH_TOKEN` and `DEEPROUTER_API_KEY`

`~/.codex/config.toml` stores the name `DEEPROUTER_API_KEY`, not the key. `GET /v1/models` and the chat completion send the key as `Authorization: Bearer`. The messages check also sends `x-api-key` and `anthropic-version`. Redirects are not followed.

`--api-key` is visible to other users on the machine through the process list. Prefer `DEEPROUTER_API_KEY`, `--api-key-stdin`, or the interactive prompt (input is hidden when the terminal allows it).

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

The config writers run on Windows. Claude Code settings go to `%USERPROFILE%\.claude\settings.json`, Codex config to `%USERPROFILE%\.codex\config.toml`, and the shell block to the PowerShell profile. `install.sh` downloads release binaries for macOS and Linux. On Windows, build with `go build -o deeprouter-setup.exe .`.

## Why Go

The tool is one static binary and the module has no third-party dependencies. Editing someone else's JSON, TOML, and shell startup file has to be idempotent and has to keep the rest of the file, which is easier to test in Go than in a shell script alone. `install.sh` is the single-file entrypoint: it builds that binary from a checkout, or downloads a release when you pipe it from the web. macOS and Linux are required; Windows is the same Go code with a PowerShell profile instead of bash.

## Development

```sh
go test ./...
go build -o deeprouter-setup .
```

CI runs the tests on Linux, macOS, and Windows, and runs `install.sh` on Linux and macOS. The core coverage is the config writers (Claude settings, Codex TOML, shell rc, backups) plus an end-to-end run of install, setup, test, switch, and restore against a local fake gateway.

Pushing a `v*` tag runs `.github/workflows/release.yml`. The workflow builds static binaries with `CGO_ENABLED=0` for darwin/amd64, darwin/arm64, linux/amd64, and linux/arm64, writes SHA256 sums to `checksums.txt`, and publishes a GitHub Release. The version embedded in each binary is the tag with the leading `v` removed (`v0.1.0` becomes `0.1.0`), via `-X main.version`.

## License

MIT. See [LICENSE](LICENSE).

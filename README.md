**English** | [简体中文](README.zh-CN.md)

# deeprouter

One command to point the coding tools you choose at an OpenAI- and Anthropic-compatible API gateway.

This repository is an open-source, one-command setup tool that connects Claude Code, Codex / ChatGPT desktop, Claude Desktop, Grok Build, and Gemini CLI to a compatible API gateway.

To use DeepRouter (deeprouter.top) with Claude Code, Codex, GPT, and more under a single key: first create a key on the [DeepRouter website](https://deeprouter.top/) (use your own key), then install and run `deeprouter` as described below and choose DeepRouter in the wizard. For common questions, see the [FAQ](https://deeprouter.top/faq).

![Terminal demo: install v0.3.8, set up Codex / ChatGPT, connectivity test passed](docs/deeprouter-v0.3.8-en.gif)

## Install

> Note: The source of truth is the public repository `WiltFan/DeepRouter`. The `https://deeprouter.top` mirror below is still available if GitHub is slow or blocked.

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/WiltFan/DeepRouter/main/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/WiltFan/DeepRouter/main/install.ps1 | iex
```

cmd:

```bat
powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/WiltFan/DeepRouter/main/install.ps1 | iex"
```

`curl ... | sh` in cmd fails with `'sh' is not recognized`. `install.sh` under Git Bash or MSYS exits and prints the PowerShell command above. Windows does not use `/dev/tty`.

The command is `deeprouter`. On macOS and Linux the binary is installed to `~/.local/bin/deeprouter`. When that directory is not on `PATH`, the installer prints an `export` line and writes it into the shell startup file (`~/.zshrc`, `~/.bashrc`, or the fish config). `--no-path` skips that step. On Windows the binary is installed to `%LOCALAPPDATA%\Programs\deeprouter\deeprouter.exe`, and that directory is added to the user PATH (registry `HKCU\Environment`, keeping the existing value type). `DEEPROUTER_NO_PATH=1` or `-NoPath` skips that step.

The previous command name was `deeprouter-setup`. The installer removes the old binary from the same directory and leaves a same-named entry that runs `deeprouter`: a `deeprouter-setup` script on macOS and Linux, and `deeprouter-setup.cmd` on Windows. The old `%LOCALAPPDATA%\Programs\deeprouter-setup` directory is removed from the user PATH.

After a successful install, a controlling terminal starts interactive setup immediately, using the absolute path of the binary just installed. `install.sh` reads from `/dev/tty` because the stdin of `curl | sh` is the install script itself. `install.ps1` starts the same wizard in the current Windows console, whose stdin is not the pipe. With no controlling terminal (CI and other non-interactive sessions) it does not start the wizard, and prints the absolute path to run next. `DEEPROUTER_NO_SETUP=1` skips the automatic start as well. That launch does not depend on `PATH` already being refreshed.

Install from a checkout:

```sh
git clone https://github.com/WiltFan/DeepRouter.git
cd deeprouter
sh install.sh
```

From a Windows checkout:

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

`install.sh` is POSIX `sh` and covers macOS and Linux. `install.ps1` is the Windows installer. Next to a checkout, both compile a static binary with Go 1.22 or newer. The version baked into the binary is `git describe --tags`, or `dev` when there is no tag. An install from the network downloads a plain release binary (not an archive) and checks it against `checksums.txt` from the same place. The default host is `https://github.com/WiltFan/DeepRouter/releases/latest/download/`. If that download fails and the repository is `WiltFan/DeepRouter`, the installer tries `https://deeprouter.top/releases/latest/`. `DEEPROUTER_DOWNLOAD_BASE` selects the first host. A mirror base uses `releases/<version-or-latest>/<asset>` rather than GitHub's `releases/download/` path. That step does not call the GitHub API. The file names are `deeprouter-darwin-amd64`, `deeprouter-darwin-arm64`, `deeprouter-linux-amd64`, `deeprouter-linux-arm64`, `deeprouter-windows-amd64.exe`, and `deeprouter-windows-arm64.exe`. `DEEPROUTER_SETUP_VERSION` without a leading `v` (for example `0.1.1`) pins GitHub to `releases/download/v0.1.1/` and a mirror to `releases/v0.1.1/`. After a fork, set `DEEPROUTER_SETUP_REPO=owner/name`. Forks do not fall back to `deeprouter.top` unless `DEEPROUTER_DOWNLOAD_BASE` is set. There is no runtime dependency after install.

## Mainland China

`raw.githubusercontent.com` and GitHub release downloads are often slow or blocked in mainland China. `https://deeprouter.top` serves the installer and the release files.

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

The copy of the installer on GitHub tries GitHub first, then `https://deeprouter.top`. The copy served by the mirror tries the mirror first, then GitHub. Each file from the first host gets 45 seconds and each file from the second gets 120 seconds, in both `install.sh` and `install.ps1`. Either way the script prints which host it used, and it still checks `checksums.txt`. A checksum mismatch, or an HTML page instead of a binary, counts as a failed download and moves on to the next host. `install.ps1` turns on TLS 1.2 for that session first, because older Windows PowerShell does not offer it by default. `DEEPROUTER_DOWNLOAD_BASE=https://deeprouter.top` forces the mirror first.

The server should serve the tree from `sh scripts/mirror-layout.sh --version v0.3.3` (upload the contents of `dist-mirror/` to the site root):

| URL | What it is |
| --- | --- |
| `https://deeprouter.top/install.sh` | installer, mirror host by default |
| `https://deeprouter.top/install.ps1` | Windows installer, mirror host by default |
| `https://deeprouter.top/releases/latest.txt` | one line, `v0.3.3` and a newline |
| `https://deeprouter.top/releases/latest/<asset>` | same bytes as the version directory |
| `https://deeprouter.top/releases/v0.3.3/<asset>` | `checksums.txt` and the six binaries |

`<asset>` is `checksums.txt`, `deeprouter-darwin-amd64`, `deeprouter-darwin-arm64`, `deeprouter-linux-amd64`, `deeprouter-linux-arm64`, `deeprouter-windows-amd64.exe`, or `deeprouter-windows-arm64.exe`. `latest/` is a real copy, not a symlink. The installer does not read `latest.txt`.

After you agree to install missing CLIs with npm, if npmjs.org may be slow, the wizard mentions `npm config set registry https://registry.npmmirror.com` and can pass `--registry https://registry.npmmirror.com` for that one install. It does not change npm's saved config. Run the `npm config set` command yourself if you want it for later installs.

## Quick start

```sh
deeprouter
```

Running it in a terminal with no arguments starts interactive setup. `install.sh` and `install.ps1` enter this step after install when a controlling terminal is present. Only the tools you select are changed.

The interface is Simplified Chinese unless the locale is clearly English: `LC_ALL`, or `LANG` when `LC_ALL` is empty, is `en`, `en_US.UTF-8`, `en-GB`, or similar. `C`, `POSIX`, and an empty locale stay Chinese. `--lang en` or `DEEPROUTER_LANG=en` selects English, and `--lang zh` or `DEEPROUTER_LANG=zh` selects Chinese, whatever the locale says. An unknown `DEEPROUTER_LANG` value is ignored. The same rule covers every command, including `setup`, `test`, and their errors, and both installers. The examples below show the English text, with the Chinese where it helps.

When stdin is not a terminal (`deeprouter </dev/null`, or a pipe), the wizard does not read the input as answers. It prints `Run deeprouter in a terminal.` (Chinese: `请在终端中运行 deeprouter。`) and tells you to set `DEEPROUTER_API_KEY` or use `deeprouter setup --api-key-stdin`. It sends no request, writes nothing, and exits with code 2.

1. Choose programs. This is the first question. More than one is allowed. The prompt shows `Example: 1,2` and `Programs [1]:` (Chinese: `多选示例：1,2（英文逗号、中文逗号、顿号或空格都可以分隔）` and `程序 [1]:`). Enter selects Codex / ChatGPT. Separators are a comma, a space, a full-width comma (`，`), an ideographic comma (`、`), or a full-width space. Codex CLI and the ChatGPT desktop app share `~/.codex/config.toml`, so the list has one entry for both. A selection with no configurable program asks again. A selected program that is not installed gets a notice that its configuration is written anyway and works after you install it, plus the official install command. This is not a failure. The notice is printed once, after the platform is chosen. `Next:` only says what to run. Lookup checks `PATH` and common npm global and Homebrew directories (`/opt/homebrew/bin`, `/usr/local/bin`, `~/.npm-global/bin`, and similar). Claude Code is `curl -fsSL https://claude.ai/install.sh | bash` (Windows: `irm https://claude.ai/install.ps1 | iex`). Codex / ChatGPT is `npm install -g @openai/codex`. Grok Build is `curl -fsSL https://x.ai/cli/install.sh | bash` (Windows: `irm https://x.ai/cli/install.ps1 | iex`). Claude Desktop is https://claude.com/download. Gemini CLI is `npm install -g @google/gemini-cli`, and macOS and Linux also have `brew install gemini-cli`.
2. Choose a platform for those programs. The prompt is `Platform [1]:` (Chinese: `平台 [1]:`). DeepRouter (`https://deeprouter.top`), WolfAI (`https://wolfai.top`), and LinkMesh (`https://www.linkmesh.org`) are always listed, because each of them supports every client. An official API is listed only when every selected program can use it: OpenAI (`https://api.openai.com`) for Codex / ChatGPT, Anthropic/Claude (`https://api.anthropic.com`) for Claude Code and Claude Desktop, Google Gemini (`https://generativelanguage.googleapis.com`) for Gemini CLI, and xAI Grok (`https://api.x.ai`) for Codex / ChatGPT and Grok Build. Selecting Codex and Claude together leaves only those three gateways. A custom base URL is always last. Enter selects DeepRouter. `DEEPROUTER_PROVIDER=wolfai` (or `linkmesh`, `openai`, `anthropic`, `gemini`, `xai`) highlights that platform when it is in the list; otherwise Enter stays on DeepRouter. OpenAI and xAI write Codex with `wire_api = "responses"` and a `base_url` that includes `/v1`. Anthropic/Claude writes `ANTHROPIC_BASE_URL` as the origin, with no `/v1`. Google Gemini writes `GOOGLE_GEMINI_BASE_URL` as the origin; the CLI appends `/v1beta`. Those presets check the token first and do not fetch a model list. The model stays the client default unless a setup flag names one. An existing Codex model line is kept, and the wizard says which model that is.
3. Gateway URL, for the custom platform only. The prompt is `Gateway URL (Enter for https://deeprouter.top, or any compatible endpoint):` (Chinese: `网关地址（直接回车使用 https://deeprouter.top，或填写其他兼容接口）：`). Any compatible endpoint is accepted. A bad URL asks again. When the model list cannot be loaded from that URL, the wizard asks for the URL again (`Gateway URL (Enter to try … again):`). Every preset, including the official APIs, skips this question and uses that platform's URL.
4. **Each selected program** gets its own token and model questions. The prompt names the platform, for example `Enter the WolfAI token (create one at wolfai.top) for Codex / ChatGPT: ` (Chinese: `请输入 WolfAI 的令牌（在 wolfai.top 创建），用于 Codex / ChatGPT：`). A custom gateway names its URL instead: `Enter the gateway token (https://gateway.example.com) for Codex / ChatGPT: ` (Chinese: `请输入该网关的令牌（https://gateway.example.com），用于 Codex / ChatGPT：`). A key saved for another platform is not offered. An existing key for this same platform is shown in brackets, as in `… for Codex / ChatGPT [sk-****Mc04]: `, and Enter keeps it (`Kept (sk-****abcd, 51 chars)`). Each typed character echoes as `*`. After submit the wizard prints `Entered (sk-****abcd, 51 chars)`. A later program with no saved key asks `Claude Code token (Enter to reuse sk-****Mc04): ` (Chinese: `Claude Code 令牌（直接回车沿用 sk-****Mc04）：`); Enter uses the token chosen for the previous program and prints `Reused (sk-****abcd, 51 chars)`. An empty answer prints `The token is empty. Enter it again.` (Chinese: `令牌不能为空，请重新输入。`) and asks again.

   The token is checked before anything else: the wizard fetches the model list and sends tiny test requests. A 401 or 403 prints the gateway's reason, for example `The gateway rejected this token (401). Gateway said: Invalid token` (Chinese: `网关拒绝了这个令牌（401）。网关返回：Invalid token`), or `The gateway refused this token (403): no quota left, or no access.`, and asks for the token again. The test requests probe up to eight of the family's chat models at once, newest version first, each with a short timeout. A gateway recommendation does not jump ahead of a newer id, so gemini-3.8-flash is tried before gemini-2.5 even when the list starts with the older id. A model that times out is skipped and does not make the others wait. GPT models older than gpt-5.4 (gpt-4.x and gpt-5.0 through gpt-5.3, including names such as gpt-4.1-mini) are not candidates. gpt-5.4, gpt-5.4-mini, and anything newer stay in. Codex sends the same request Codex uses, POST /v1/responses with reasoning.effort set to medium, so a model that rejects that parameter fails here and is skipped. A model that fails is reported, for example `Skipped gpt-5.5: 503 No channel. Gateway said: …` (Chinese: `已跳过 gpt-5.5：503 没有可用渠道。网关返回：…`), and left out of the list. The model question looks like this:

   ```text
   Codex / ChatGPT model (Enter does not set a model; the client keeps its own default):
     1) Do not set a model (use the client default)
     2) gpt-5.5  (answered a test request)
     3) gpt-5.4  (answered a test request)
   Codex / ChatGPT model [1]:
   ```

   Grok Build does not offer that first option. Its list starts with the models that answered, newest first, and Enter selects the first of them. The word `default` is not accepted. When none answered, type a model id; Enter alone does not leave the model unset. `deeprouter setup --apps grok` without `--grok-model` writes that newest model. If none answered, setup exits 1 and writes nothing. Claude Code, Claude Desktop, Gemini CLI, and Codex / ChatGPT keep option 1. Official presets still skip the model question and leave Grok Build's model unset, because they do not fetch a model list.

   Official presets skip this question. They check the token with one request first. A refused token asks again and writes nothing. That includes HTTP 401, HTTP 403, and a provider response that says the key is invalid, such as Gemini HTTP 400 `API_KEY_INVALID` or xAI HTTP 400 incorrect API key. `--skip-verify` does not accept a refused token. If the official API cannot be reached (no response, or a timeout), the wizard warns that these sites need a reachable network (from mainland China, usually a network outside China) and asks `Continue and write the config without checking the token? [y/N]` (Chinese: `不检查令牌，继续写入配置？[y/N]`). A response from the server is not that case, and the overseas-network hint is not shown. No writes nothing and exits 1, and says to check the network or proxy and run setup again. A run without a terminal is the one that mentions `--skip-verify`. When an existing Codex model line is kept, the wizard says that model is kept. Otherwise it prints that the client keeps its own default. When Codex / ChatGPT already has a root `model` line, option 1 is `Keep the existing model gpt-5` (Chinese: `保留已有模型 gpt-5`) and Enter keeps that line. Otherwise option 1 is `Do not set a model (use the client default)` (Chinese: `不指定模型（使用客户端默认）`). Every model that answered is marked `（已通过测试）` in Chinese. Option 1, Enter, or the word `default` writes no model. For Codex / ChatGPT that leaves an existing root `model` line unchanged. For the other programs except Grok Build it removes a model line written earlier, so the program uses its own default. Every listed model answered a test request. When none answered, the prompt says timed-out and failed models were skipped, the list is only option 1, and a typed id is tested once, including an id that just timed out. A listed id can also be typed. Anything else, such as `n`, `yes`, or a number outside the list, prints `That is not a model id. Enter a listed number, a listed id, or press Enter.` (or `Model 99 is out of range. …`) and asks again. When the gateway lists no model of that family, Enter leaves the model unset and a typed id is accepted. Image, video, audio, embedding, speech, and moderation models are omitted, including ids containing image, nano-banana, dall-e, sora, veo, imagen, embedding, tts, whisper, audio, realtime, moderation, flare, or sunburst. Type or capability fields on the model object win over the id. If a family is filtered down to nothing, the picker falls back to that family's unfiltered list. One key fetches the model list once, and each family is tested once per key.
5. Before anything is written, the wizard lists the files it will change and prints `A backup is saved first. Undo with: deeprouter restore` (Chinese: `修改前会留下备份。撤销请运行：deeprouter restore`). It then writes. There is no `Continue? [Y/n]` question. `deeprouter setup` does the same: it prints the file list and the restore command, and writes without asking. The terminal wizard does not edit the shell startup file, except for Gemini CLI, which reads its key and base URL from the environment. GUI apps on macOS do not read `~/.zshrc`.
6. The files are written and, for a DeepRouter-style gateway, the connectivity test runs for the selected programs. Official presets do not run that model-list probe; the token was already checked. A program with no model set is tested with a model that answers, shown as `gpt-5.4  (test only, not saved)` (Chinese: `（仅用于测试，未写入配置）`); that id is not written. If the test fails, the wizard says that the files were already written and offers `1) Pick another model` and `2) Roll back the files just written` (`Choice [2]:`). `1` asks again for the model of each program whose check failed, writes it, and runs the test again. Enter, `2`, or Ctrl-D rolls back to the files from before this run and exits with code 1.
7. If an npm CLI among the selected programs is missing, the wizard looks for `npm` first. When `npm` is not on `PATH`, it does not ask about installing with npm or about npmmirror. It prints how to install Node.js, which includes npm (`https://nodejs.org/`), and the wizard still exits 0. When `npm` is present, it asks `Install missing CLIs with npm? [y/N]` (Chinese: `用 npm 安装缺少的命令行工具？`). Anything other than `y` installs nothing. After a yes, when npmjs.org may be slow (a mainland-China locale or time zone, or npmjs.org not answering), the wizard mentions `npm config set registry https://registry.npmmirror.com` and asks `Use https://registry.npmmirror.com for this install only?`. Both questions default to no (`[y/N]`), including in a mainland-China locale. Before the install question, the wizard prints the exact `npm install -g` command. A yes to the mirror question adds `--registry` for that one install and says that this does not change the saved npm config. The wizard never runs `npm config set`.

Ctrl-D at any question before the write cancels with `Setup cancelled. Nothing was written.` and exit code 2. Ctrl-D on the token question after that token was already rejected is the exception: it exits 1 with `The token was rejected. Nothing was written.` (Chinese: `令牌被拒绝。没有写入配置。`) and does not print the cancel line. Ctrl-C before the write prints the cancel line and exits with code 130. Ctrl-C after the write, before the connectivity test has passed, rolls back the files just written and exits with code 130. After the test has passed, Ctrl-C (for example at the npm question) keeps the files and says so.

Scripts use `setup` and flags:

```sh
export DEEPROUTER_API_KEY=sk-your-key
deeprouter setup
deeprouter setup --provider wolfai
deeprouter setup --provider linkmesh --base-url https://example.com
deeprouter setup --provider openai
deeprouter setup --provider anthropic
deeprouter setup --provider gemini
deeprouter setup --provider xai --apps codex,grok
deeprouter setup --apps codex,claude-code
deeprouter setup --apps grok --grok-model grok-4 --grok-key "$GROK_KEY"
deeprouter test
```

Each preset has its own token. In a terminal, `setup` with no key asks once and names the site, for example `Enter the WolfAI token (create one at wolfai.top): ` (Chinese: `请输入 WolfAI 的令牌（在 wolfai.top 创建）：`). A key saved for another provider is not reused. `DEEPROUTER_API_KEY` is the DeepRouter token only. LinkMesh is a separate gateway: connectivity checks call `/v1/models` and the chat routes, not new-api `/api/status`.

LinkMesh (`https://www.linkmesh.org`) runs sub2api. Without a key, `POST /v1/chat/completions`, `POST /v1/responses`, and `POST /v1/messages` answer 401 `API_KEY_REQUIRED`, and `POST /v1beta/models/{model}:generateContent` answers 401 in Gemini's error shape. `POST /v1/models/{model}:generateContent` is 404, so Gemini is pointed at the origin and the CLI calls `/v1beta`. Codex uses `wire_api = "responses"` and `base_url` `https://www.linkmesh.org/v1`. Claude Code and Claude Desktop use `https://www.linkmesh.org` with no `/v1`. Grok Build uses the chat-completions route. All five programs are written. A program whose route is missing is skipped, with a message such as `LinkMesh does not expose POST /v1/messages, so Claude Code is skipped.`

`setup` without `--apps` writes the provider's default program: Codex / ChatGPT for the gateways, OpenAI, and xAI; Claude Code and Claude Desktop for Anthropic; Gemini CLI for Google Gemini. Enter on the wizard's program list is always Codex / ChatGPT, because that question comes before the platform. `--apps` writes only the listed programs, and `--claude-only` writes Claude Code and Claude Desktop. `--claude-only` and `--codex-only` cannot be combined with `--apps`. `setup` does not edit the shell startup file, except for Gemini CLI. A model flag that is left out writes no model, the same as `default`. Codex / ChatGPT keeps an existing root `model` line in that case. An explicit model id replaces it. The closing hint mentions the shell startup file only when this run wrote one. If an npm global command is installed but its directory is not on `PATH`, the hint shows how to add that directory and the full path.

Before it writes anything, `setup` checks the token on a DeepRouter-style gateway: `Checking the token at <gateway> ...` (Chinese: `正在确认 <网关> 的令牌……`), then the model list and one tiny request per program. Official presets (`openai`, `anthropic`, `gemini`, `xai`) send one request to the vendor models endpoint and do not offer a model list. OpenAI and xAI use `Authorization: Bearer`. Gemini uses `x-goog-api-key`. Anthropic uses `x-api-key` and `anthropic-version`, not `Authorization`. A 200 means the token was accepted. A refused token prints that it was rejected, writes nothing, and exits 1. That includes HTTP 401, HTTP 403, and a provider response that says the key is invalid, such as Gemini HTTP 400 `API_KEY_INVALID` or xAI HTTP 400 incorrect API key. The token is not printed. `--skip-verify` does not override a refused token. If the official API cannot be reached (no response, or a timeout), setup says that these APIs need a network that can reach them (from mainland China, usually a network outside China). A response with some other HTTP status is not a network problem, and that hint is not shown. In a terminal it asks whether to continue. Without a terminal it exits 1 unless `--skip-verify` is set. A model flag is stored as given. When Codex already has a root `model` line and the flag is omitted, setup says that model is kept. A 401 or 403, or a model passed with a `--*-model` flag that does not answer, prints the gateway's reason with `Nothing was written.` (Chinese: `没有写入配置。`) and exits with code 1. A program with no model set is tested with a model that answers, and that id is not written; when none answers, `setup` says so and writes anyway. Grok Build is the exception: the newest model that answered is written, and when none answered nothing is written. When the gateway cannot be reached at all, `setup` says that the token was not confirmed, writes the configuration anyway, and suggests `deeprouter test` for when the gateway is reachable.

## Commands

| Command | What it does |
| --- | --- |
| `setup` | Write the selected programs for one profile |
| `test` | Check the programs saved on the active profile: `GET /v1/models`, then one single-token request per program. Codex / ChatGPT uses `POST /v1/responses` (`POST /v1/chat/completions` with `wire_api = "chat"`), Claude Code and Claude Desktop `POST /v1/messages`, Grok Build `POST /v1/chat/completions`, and Gemini CLI `POST /v1beta/models/{model}:generateContent` with the `x-goog-api-key` header. A program with no model set is tested with a chat model of its family that answers, marked `(test only, not saved)`. Image, video, audio, embedding, speech, and moderation ids are not used for that; the unfiltered list is the fallback when the chat filter leaves nothing. Prints HTTP status, time to first byte, and total time |
| `models` | List model ids on the gateway |
| `switch` | Switch profile, or change one program's model |
| `profile list\|add\|use\|remove` | Named profiles. One profile stores one API key |
| `status` | Active profile, which tools are wired, and whether `claude` and `codex` are installed (with versions) |
| `restore` | Put changed files back from a backup |
| `uninstall` | Restore those files. `--purge` also deletes `~/.deeprouter` |
| `version` | Print the version |

`deeprouter <command> --help` lists flags. `test` and `models` accept `--json`. `--lang zh` or `--lang en` works with every command and with the wizard.

Exit codes: `0` success. A wizard that cannot find `npm` still exits 0 after printing how to install Node.js. `1` a gateway check failed, the token was refused, an official API could not be reached and `--skip-verify` was not set, a file write failed, the wizard rolled back its write, `setup --install-clis` could not find `npm`, or Ctrl-D was pressed on the token question after that token was already rejected. `2` the command line is wrong, setup was cancelled before the write (Ctrl-D, except the rejected-token re-prompt), or the wizard was started without a terminal. `130` Ctrl-C.

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
| `--provider NAME` | `deeprouter` (default, `https://deeprouter.top`), `wolfai` (`https://wolfai.top`), `linkmesh` (`https://www.linkmesh.org`), `openai` (`https://api.openai.com`), `anthropic` (`https://api.anthropic.com`), `gemini` (`https://generativelanguage.googleapis.com`), or `xai` (`https://api.x.ai`). `DEEPROUTER_PROVIDER` selects the same preset. `--base-url` overrides the URL and keeps this id. Codex writes `model_provider = "<id>"` and `[model_providers.<id>]`. Grok Build writes `[model.<id>]`. Configuring again removes the previous managed section (`deeprouter`, `wolfai`, `linkmesh`, `openai-api`, `anthropic`, `gemini`, `xai`, or `custom`) and leaves unrelated tables alone. `[model_providers.openai]` is not one of those sections. The four official names check the token, do not offer a model list, and are offered only for the clients that speak that API. `--skip-verify` writes when that check cannot reach the API. A refused token, including a provider HTTP 400 that says the key is invalid, still writes nothing. `install.sh` and `install.ps1` pass `--provider` and `DEEPROUTER_PROVIDER` through to setup |
| `--apps LIST` | `claude-code`, `claude-desktop`, `codex`, `grok`, `gemini`, separated by commas or spaces |
| `--api-key` / `--api-key-stdin` / `DEEPROUTER_API_KEY` | Shared key. Used for any program that has no key of its own |
| `--claude-code-key`, `--claude-desktop-key`, `--codex-key`, `--grok-key`, `--gemini-key` | Written only to that program. Omitted flags use the shared key |
| `--claude-model`, `--claude-desktop-model`, `--codex-model`, `--grok-model`, `--gemini-model` | Model id for that program, checked with one tiny request before anything is written. A model that does not answer stops `setup` with exit code 1 and writes nothing. When the flag is omitted, or is `default`, no model is written, except `--grok-model`: omitting it on a gateway writes the newest Grok Build model that answered a test request, and `default` is rejected. Codex / ChatGPT leaves an existing root `model` line unchanged; pass a model id to replace it. The other programs except Grok Build remove a model line written earlier so the program uses its own default: Claude Code loses `ANTHROPIC_MODEL` and Gemini CLI `model.name`. Claude Desktop loses a single-entry `inferenceModels` and gets `modelDiscoveryEnabled`; a list of more than one model is left alone. `--claude-model` also applies to Claude Desktop when `--claude-desktop-model` is not given |
| `--no-shell` | Kept for older scripts. `setup` edits the shell startup file only for Gemini CLI, and `--no-shell` does not skip that, because Gemini CLI reads `GEMINI_API_KEY` and `GOOGLE_GEMINI_BASE_URL` from the environment |
| `--claude-only` | Write Claude Code and Claude Desktop, and skip Codex / ChatGPT. Cannot be combined with `--apps` |
| `--codex-only` | Write only Codex / ChatGPT |

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

The model ids below are examples. Use an id your gateway actually lists. A model line is written only when you pick a model in the wizard or pass a `--*-model` flag; by default it is left out. `--base-url` may include `/v1` or not: Claude receives the URL without a trailing `/v1` (the client requests `/v1/messages` itself), and Codex / ChatGPT and Grok Build receive the URL with `/v1`.

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

`inferenceGatewayBaseUrl` has no trailing `/v1`. The in-app example has this shape (`https://llm-gateway.example.corp`). Code itself requests `POST /v1/messages`. `GET /v1/models` is optional. A single model id is written to `inferenceModels`, unless the file already lists more than one model. With no model chosen, a single-entry `inferenceModels` is removed and `modelDiscoveryEnabled` is set to true, so the app lists the gateway's models itself; a list of more than one model is left alone. Discovery shows only Claude model ids the app recognizes.

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

The id follows the provider. `--provider wolfai` writes `model_provider = "wolfai"` and `[model_providers.wolfai]` with `base_url = "https://wolfai.top/v1"`. LinkMesh uses `linkmesh` and `https://www.linkmesh.org/v1`. `--provider openai` writes `model_provider = "openai-api"` and `[model_providers.openai-api]` with `base_url = "https://api.openai.com/v1"` and `wire_api = "responses"`. The section is `openai-api` so a Codex config that already has `[model_providers.openai]` keeps that table. `--provider xai` uses `xai` and `https://api.x.ai/v1`, also `responses`, because xAI serves `POST /v1/responses` and current Codex rejects `wire_api = "chat"`. A custom wizard URL uses `custom`. The next configure removes the previous managed section and does not touch `[model_providers.openai]` or other tables.

Codex removed `disable_response_storage` (requests are never stored). This tool does not write that key. The next Codex / ChatGPT configure removes it from the root table and from the managed provider section. The same key in any other table is left as it is.

The key is `experimental_bearer_token`. The app server and processes started from the GUI read `config.toml` directly and do not need an environment variable. `env_key` is not written: if it is present, Codex returns `Missing environment variable: DEEPROUTER_API_KEY` before it looks at the bearer token.

`~/.codex/auth.json` (or the system keychain when `cli_auth_credentials_store` is `keyring` or `auto`) is the ChatGPT / OpenAI API key login cache, not credentials for a custom provider. This tool does not write the gateway key there.

Codex / ChatGPT uses the Responses API, so `wire_api` defaults to `responses`. Current Codex accepts only `responses` and rejects `wire_api = "chat"`. Older builds can still pass `--codex-wire-api chat`. The wizard's connectivity test calls only the endpoints for the programs selected in that run. Codex / ChatGPT checks `GET /v1/models` and `POST /v1/responses` (`POST /v1/chat/completions` with `wire_api = "chat"`), the same API Codex uses. Claude Code and Claude Desktop check `POST /v1/messages`. Grok Build checks `POST /v1/chat/completions`. Gemini CLI checks `POST /v1beta/models/{model}:generateContent` with `x-goog-api-key`. When no model is set, the test uses a chat model of that family that answers, and does not save it. An endpoint whose family has no model in the gateway list is skipped. `deeprouter test` checks the programs saved on the active profile.

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
- A selected model is written to `model.name`. Model option 1, Enter, an omitted `--gemini-model`, or `--gemini-model default` deletes `model.name` so Gemini CLI uses its own default. Other fields under `model` stay.

When it is not installed, the wizard and `setup` print the official command `npm install -g @google/gemini-cli`. Docs: https://geminicli.com/docs/get-started/installation/. The configuration is still written. A project `.gemini/settings.json` overrides user settings. System settings are `/etc/gemini-cli/settings.json` on Linux, `C:\ProgramData\gemini-cli\settings.json` on Windows, and `/Library/Application Support/GeminiCli/settings.json` on macOS. Docs: [configuration](https://geminicli.com/docs/reference/configuration/).

### Shell startup file

On macOS and Linux, a marked export block in `~/.zshrc`, `~/.bashrc`, or `~/.config/fish/config.fish` is written only when Gemini CLI is configured, because Gemini CLI reads `GEMINI_API_KEY` and `GOOGLE_GEMINI_BASE_URL` from the environment. An already-open terminal can `source` it. The wizard and `setup` do not write that block for Claude Code or Codex / ChatGPT, which read their own files. Variables an earlier version put in the block are kept until `restore`. Windows does not edit `.zshrc` or a PowerShell profile. The same variables are written to the current user's environment (registry `HKCU\Environment`, the same effect as `setx` without the 1024-character truncation). A newly started process inherits them. `restore` and `uninstall` put those variables back to the values from before the first write. With `--rc-file`, Windows edits that startup file and does not write user environment variables. Claude Code, Codex / ChatGPT, Claude Desktop, and Grok Build do not depend on this export.

```sh
# >>> deeprouter-setup >>>
export GOOGLE_GEMINI_BASE_URL="https://deeprouter.top"
export GEMINI_API_KEY="sk-your-key"
# <<< deeprouter-setup <<<
```

Running again replaces that block. It does not append a second copy.

## What stays on the vendor

A normal Claude.ai chat does not use a custom gateway. Claude Code in Slack, and Claude Code cloud sessions, are outside a gateway deployment: gateway variables in a cloud session have no effect. Remote Control is unavailable while the desktop gateway configuration is active. Those surfaces keep using Anthropic.

Managed policy wins over local files: Claude Desktop MDM / `/etc/claude-desktop/managed-settings.json`, and Grok Build `managed_config.toml`, `requirements.toml`, and MDM. This tool does not write those locations.

## FAQ

### Where does the API key go?

The key goes only into the files of the programs you configure, and into the profile:

- `~/.deeprouter/config.json`, mode `0600`, always. When programs use different keys, the profile stores the overrides as well
- Codex / ChatGPT: `experimental_bearer_token` under `[model_providers.<id>]` (`[model_providers.deeprouter]` for DeepRouter) in `~/.codex/config.toml`. The Codex / ChatGPT CLI, desktop app, and IDE extension read it. After the write, the value is checked against the profile, and the provider must not have `env_key`. `auth.json` is left alone
- Claude Code: `env.ANTHROPIC_AUTH_TOKEN` in `~/.claude/settings.json` for a gateway. The official Anthropic preset writes `env.ANTHROPIC_API_KEY` and removes `ANTHROPIC_AUTH_TOKEN`. The Claude Code CLI reads it. The Code tab in Claude Desktop does not use it as the gateway credential
- Claude Desktop: `inferenceGatewayApiKey` in the active file under `configLibrary/`. Gateways set `inferenceGatewayAuthScheme` to `bearer`. The official Anthropic preset sets `x-api-key`
- Grok Build: `api_key` in the managed `[model.<id>]` table of `~/.grok/config.toml`
- Gemini CLI: `GEMINI_API_KEY` in the shell startup file. On Windows it lives in the user environment, not in a startup file

Enter at the wizard's program question configures Codex / ChatGPT only. `setup` without `--apps` writes the provider's default program. `GET /v1/models`, the Responses request, and the chat completion use `Authorization: Bearer`. The messages check also sends `x-api-key` and `anthropic-version`, and the Gemini check sends `x-goog-api-key`. The official Anthropic token check sends only `x-api-key` and `anthropic-version`. Redirects are not followed. `DEEPROUTER_API_KEY` is read only for DeepRouter. For any other provider, a missing key says to pass `--api-key` or `--api-key-stdin`.

`deeprouter status` prints whether `claude` and `codex` are on `PATH`, and the first line of each `--version`. A selected program that is not installed is noted once, after the platform is chosen, and only for the programs selected in that run. `Next:` does not repeat that note. When `npm` is on `PATH`, the wizard prints the exact `npm install -g` command and asks `Install missing CLIs with npm? [y/N]`. The default is no in every locale. A missing program is described as configuration that works once it is installed. Codex / ChatGPT notes that the desktop app uses the same config, so answering N is fine when only the desktop app is used. When `npm` is not on `PATH`, the wizard does not ask about npm or npmmirror; it prints how to install Node.js and still exits 0. Claude Desktop has no npm package. Grok Build prints its own installer instead of joining the npm prompt. `setup --install-clis` runs `npm install -g` for the missing npm CLIs among the programs being configured (`@anthropic-ai/claude-code`, `@openai/codex`, `@google/gemini-cli`). With no `npm`, it prints the official install pages, installs nothing, and exits 1.

When a key already exists for the same platform, the wizard's prompt names that site and shows the key, for example `Enter the WolfAI token (create one at wolfai.top) for Codex / ChatGPT [sk-****Mc04]: `. Enter keeps it. A key saved for a different platform is not reused. `DEEPROUTER_API_KEY` is the DeepRouter token only. `setup` asks only when it has no key and stdin is a terminal. Otherwise it uses `--api-key` or `--api-key-stdin`, the key already saved for this provider, or `DEEPROUTER_API_KEY` when the provider is DeepRouter. LinkMesh is not called via `/api/status`.

`~/.deeprouter/backups` holds a copy of each file from before a change, so it can contain a key that used to live in `settings.json` or a shell rc. `deeprouter uninstall --purge` deletes that directory.

### How do I undo this?

`deeprouter restore` writes back the copy from before this tool first changed each file. Files this tool created are deleted. Directories that did not exist before, and are empty after those files are removed, are deleted too. A directory that already existed, or that still has other files, stays. Profiles stay, so you can run `setup` again.

`deeprouter restore --list` lists later snapshots and shows each time in local time. `deeprouter restore --help` documents `--list` and `--id`. `deeprouter restore --id latest` undoes only the most recent change.

`deeprouter uninstall` does the same restore. `--purge` deletes `~/.deeprouter`, including saved profiles and backups.

### Can I use another gateway?

Yes. Any base URL that serves `GET /v1/models` and the routes of the programs you configure works: `POST /v1/responses` for Codex / ChatGPT, `POST /v1/messages` for Claude Code and Claude Desktop, `POST /v1/chat/completions` for Grok Build, and `POST /v1beta/models/{model}:generateContent` for Gemini CLI. In the wizard, choose `4` and type the URL; the token prompt then names that URL.

```sh
deeprouter setup --base-url https://gateway.example.com --api-key "$GATEWAY_KEY"
```

`test` reports the HTTP status and a short hint, followed by the gateway's own message: `401` bad key, `403` quota or permission, `404` model or path, `429` rate limit, `503` no channel available for that model.

### Does Windows work?

Yes. Releases include `deeprouter-windows-amd64.exe` and `deeprouter-windows-arm64.exe`, with checksums in `checksums.txt`. Install with the PowerShell or cmd command above. Interactive questions use the Windows console: each API key character is shown as `*` (the console reads character by character; there is no `stty` and no `/dev/tty`). Under `irm ... | iex` the installer runs inside your PowerShell session, so it returns to the prompt when setup ends, whatever you answered, and does not close the window. Run as a file (`-File .\install.ps1`), it exits with the wizard's exit code and prints nothing more when you cancel.

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

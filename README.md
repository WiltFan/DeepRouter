# deeprouter

一条命令，把兼容 OpenAI 与 Anthropic 的 API 网关接到你选定的编程工具上。

![deeprouter configuring Claude Code and Codex](docs/demo.gif)

发布到 Product Hunt 和 Show HN 之前，把 `docs/demo.gif` 换成一段短录屏。

## 安装

macOS 和 Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.sh | sh
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex
```

cmd：

```bat
powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex"
```

在 cmd 里执行 `curl ... | sh` 会报 `'sh' is not recognized`。Git Bash 和 MSYS 里的 `install.sh` 也会直接退出，并打印上面的 PowerShell 命令。Windows 不用 `/dev/tty`。

命令名是 `deeprouter`。macOS 和 Linux 的二进制装到 `~/.local/bin/deeprouter`。这个目录不在 `PATH` 上时，安装程序会打印 `export` 行，并把它写进 shell 启动文件（`~/.zshrc`、`~/.bashrc` 或 fish 配置）。加 `--no-path` 可以跳过这一步。Windows 的二进制装到 `%LOCALAPPDATA%\Programs\deeprouter\deeprouter.exe`，并把这个目录加进用户 PATH（注册表 `HKCU\Environment`，保留原来的值类型）。`DEEPROUTER_NO_PATH=1` 或 `-NoPath` 可以跳过。

以前的命令名是 `deeprouter-setup`。安装程序会删掉同目录里的旧二进制，并留下一个同名入口，转而执行 `deeprouter`：macOS 和 Linux 是 `deeprouter-setup` 脚本，Windows 是 `deeprouter-setup.cmd`。旧的 `%LOCALAPPDATA%\Programs\deeprouter-setup` 会从用户 PATH 里去掉。

安装成功后，如果当前进程有控制终端，会立刻用刚装好的绝对路径启动交互式配置。`install.sh` 从 `/dev/tty` 读输入，因为 `curl | sh` 的标准输入是安装脚本本身。`install.ps1` 在当前 Windows 控制台里启动同一个向导，标准输入没有被管道占住。没有控制终端时（CI、其他非交互会话）不启动，改为打印下一步要运行的绝对路径。设置 `DEEPROUTER_NO_SETUP=1` 同样跳过自动启动。这一次启动不依赖 `PATH` 是否已经刷新。

从仓库安装：

```sh
git clone https://github.com/zfg5118/deeprouter.git
cd deeprouter
sh install.sh
```

Windows 仓库里：

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

`install.sh` 是 POSIX `sh`，只覆盖 macOS 和 Linux。`install.ps1` 是 Windows 安装程序。在仓库旁边时，两者都用 Go 1.22 或更新版本编译一个静态二进制。二进制里的版本号是 `git describe --tags`；没有标签时是 `dev`。从网上安装时，它们从 `https://github.com/zfg5118/deeprouter/releases/latest/download/` 下载普通的发布二进制（不是压缩包），并用同一路径下的 `checksums.txt` 校验。这一步不调用 GitHub API。文件名是 `deeprouter-darwin-amd64`、`deeprouter-darwin-arm64`、`deeprouter-linux-amd64`、`deeprouter-linux-arm64`、`deeprouter-windows-amd64.exe`、`deeprouter-windows-arm64.exe`。`DEEPROUTER_SETUP_VERSION` 不带前导 `v`（例如 `0.1.1`）会固定到 `releases/download/v0.1.1/`。Fork 之后设置 `DEEPROUTER_SETUP_REPO=owner/name`。安装完成后没有运行时依赖。

## 快速开始

```sh
deeprouter
```

在终端里不带参数运行，会进入交互式配置。`install.sh` 在有控制终端时会在装完后直接进入这一步。程序只改你勾选的那些工具，其余文件不动。

1. 先选择要配置的程序，可以多选。直接回车等于 `1,2,3`：Claude Code、Claude Desktop、Codex。Codex 的 CLI 和桌面应用共用同一个文件，所以列表里只有一项。Grok Build 和 Gemini CLI 要单独选。选出来的结果里一个可配置程序都没有时，会重新问，而不是直接退出。
2. 网关地址。提示原文是 `Gateway 地址（回车使用 https://deeprouter.top，也可填其他兼容的接口地址）:`。回车使用 `https://deeprouter.top`。也可以填其他兼容的接口地址。格式不对会要求重填。
3. **对每一个选中的程序单独**询问 API key 和模型。已有 key 时提示类似 `Claude Code API key [sk-n…Mc04]:`。输入时每个字符回显一个 `*`。提交后打印 `已输入 (sk-****abcd, 51 chars)`。回车保留当前 key，并打印 `已保留` 和同样的掩码。模型列表的第 1 项是使用该程序自己的默认模型，回车就是这一项：不写模型字段，并删掉本工具以前写过的模型。其它序号是网关列出的 id。同一个 key 只拉一次模型列表。
4. 选中的程序如果还没安装，会在开始配置之前打印说明和官方安装命令，配置仍会写上。结束时的 `Next:` 里会再打印一次，而不只是 `Then run:`。查找时除了当前 `PATH`，还会看常见的 npm 全局目录和 Homebrew 目录（`/opt/homebrew/bin`、`/usr/local/bin`、`~/.npm-global/bin` 等）。Claude Code 是 `curl -fsSL https://claude.ai/install.sh | bash`（Windows 是 `irm https://claude.ai/install.ps1 | iex`），Codex 是 `npm install -g @openai/codex`，Grok Build 是 `curl -fsSL https://x.ai/cli/install.sh | bash`（Windows 是 `irm https://x.ai/cli/install.ps1 | iex`），Claude Desktop 是 https://claude.com/download ，Gemini CLI 是 `npm install -g @google/gemini-cli`，macOS 和 Linux 另外还有 `brew install gemini-cli`。
5. 只把选中程序的配置写盘，然后做连通性测试。终端向导不改 shell 启动文件。macOS 上的图形界面本来也不会读 `~/.zshrc`。
6. 如果 `claude` 或 `codex` 没安装，向导结束时仍会先问再装（`y/N`）。不回答 `y` 就不会安装。

非交互（脚本、stdin 不是终端）用 `setup` 和标志，不走上面的问答：

```sh
export DEEPROUTER_API_KEY=sk-your-key
deeprouter setup
deeprouter setup --apps grok --grok-model grok-4 --grok-key "$GROK_KEY"
deeprouter setup --claude-model default --codex-model default
deeprouter test
```

不带 `--apps` 的 `setup` 仍会写入 Claude Code、Claude Desktop、Codex 和 shell 启动文件，和以前的脚本兼容。`--apps` 只写列出的程序，并且不改 shell。`--claude-only` 和 `--codex-only` 不能和 `--apps` 一起用。

## 命令

| 命令 | 作用 |
| --- | --- |
| `setup` | 为一个 profile 写入所选程序的配置 |
| `test` | `GET /v1/models`、一次单 token 的 chat completion、一次单 token 的 messages。配置过 Gemini CLI 时再加上 `POST /v1beta/models/{model}:generateContent`（请求头 `x-goog-api-key`；模型用已选的 id，没选则用模型列表里的 Gemini id）。打印 HTTP 状态、首字节时间和总时间 |
| `models` | 列出网关上的模型 id |
| `switch` | 切换 profile，或改某一个程序的模型 |
| `profile list\|add\|use\|remove` | 管理具名 profile。一个 profile 保存一把 API key |
| `status` | 当前 profile、每个工具是否已接上、`claude` 和 `codex` 是否已安装（含版本） |
| `restore` | 从备份还原被改过的文件 |
| `uninstall` | 还原这些文件。`--purge` 同时删除 `~/.deeprouter` |
| `version` | 打印版本 |

`deeprouter <command> --help` 可以看标志。`test` 和 `models` 接受 `--json`。

退出码：`0` 成功，`1` 网关检查或写文件失败，`2` 命令行不正确。

## 非交互标志

```sh
deeprouter setup \
  --base-url https://deeprouter.top \
  --apps claude-code,claude-desktop,codex,grok \
  --api-key "$DEEPROUTER_API_KEY" \
  --claude-model claude-sonnet-4-6 \
  --claude-desktop-model claude-opus-4-6 \
  --codex-model gpt-5.4 \
  --grok-model grok-4 \
  --claude-code-key "$CLAUDE_KEY" \
  --claude-desktop-key "$DESKTOP_KEY" \
  --codex-key "$CODEX_KEY" \
  --grok-key "$GROK_KEY"
```

| 标志 | 作用 |
| --- | --- |
| `--apps LIST` | `claude-code`、`claude-desktop`、`codex`、`grok`、`gemini`，逗号或空格分隔 |
| `--api-key` / `--api-key-stdin` / `DEEPROUTER_API_KEY` | 各程序共用的 key。某个程序没单独给 key 时用它 |
| `--claude-code-key`、`--claude-desktop-key`、`--codex-key`、`--grok-key`、`--gemini-key` | 只写入对应程序。省略时用共用 key |
| `--claude-model`、`--claude-desktop-model`、`--codex-model`、`--grok-model`、`--gemini-model` | 对应程序的模型 id。传 `default` 时不写模型字段，并删掉本工具以前写过的模型，让程序用自己的默认模型。省略时从 `/v1/models` 里挑一个。Grok 的列表里没有包含 `grok` 的 id 时，必须传 `--grok-model` 或 `--grok-model default`。Gemini 列表里没有包含 `gemini` 的 id 时，不写 `model.name` |
| `--no-shell` | 不改 Claude / Codex 用的那一组环境变量。选了 Gemini CLI 时，`GEMINI_API_KEY` 和 `GOOGLE_GEMINI_BASE_URL` 仍会写入同一段，因为 Gemini CLI 从环境变量读它们 |
| `--claude-only` | 写 Claude Code 和 Claude Desktop，不写 Codex。不能和 `--apps` 一起用 |
| `--codex-only` | 只写 Codex，不写 Claude Code 和 Claude Desktop |

`--api-key` 会出现在进程列表里。脚本里更适合用 `DEEPROUTER_API_KEY` 或 `--api-key-stdin`。

`switch` 只更新上次实际写入过的那些程序。一个只配过 Grok Build 的 profile，切换时不会改 Claude Code 或 Codex。模型可以用 `--claude-model`、`--claude-desktop-model`、`--codex-model`、`--grok-model`、`--gemini-model` 单独改。

## Profile

一个 profile 保存网关地址、API key 和可选的模型 id。名字自己定，不再有 official-key 分组。

```sh
deeprouter setup --api-key "$DEEPROUTER_API_KEY"
deeprouter profile add azure --label Azure --api-key "$AZURE_KEY"
deeprouter switch --profile azure
```

切换 profile 会重写上次配置过的那些文件。重复运行会就地替换，不会在 `~/.zshrc` 里追加第二段，也不会把同一个 key 写两遍。

## 每个程序写到哪里

下面的模型 id 以你确认过的网关列表为准。带 `/v1` 或不带都可以作为 `--base-url`：Claude 拿到的地址没有末尾 `/v1`（客户端自己请求 `/v1/messages`），Codex 和 Grok Build 拿到的地址带 `/v1`。

### Claude Code

`~/.claude/settings.json`（Windows 是 `%USERPROFILE%\.claude\settings.json`）。其它键会保留。文件模式 `0600`。这里的环境变量优先于 shell。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://deeprouter.top",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-key",
    "ANTHROPIC_MODEL": "claude-sonnet-4-6"
  }
}
```

Claude Desktop 里的 Code 标签页不用这个文件接网关。

### Claude Desktop（Chat、Cowork 和 Code 标签）

Claude Desktop 是一个应用。里面的 Claude Code 是 Code 标签，不是另一个桌面程序。官方文档里，桌面应用的网关路由来自第三方推理配置，不读 `ANTHROPIC_BASE_URL`，也不读 `settings.json`。Chat、Cowork 和 Code 共用这份配置。网关字段会传进每一次 Code 会话，不能被 `~/.claude/settings.json` 覆盖。

没有托管配置时，应用把本地配置放在第三方应用数据目录下的 `configLibrary/`（[用户身份与本地数据](https://claude.com/docs/third-party/claude-desktop/data-storage)）：

| 平台 | 目录 |
| --- | --- |
| macOS | `~/Library/Application Support/Claude-3p/configLibrary/` |
| Windows | `%APPDATA%\Claude\configLibrary/` |
| Linux | `~/.config/Claude-3p/configLibrary/` |

Windows 使用和 macOS Claude-3p 相同的 `configLibrary` 布局（`_meta.json` 加上每份配置一个 `.json`）。目录放在 `%APPDATA%\Claude` 下，也就是 Windows 上对应 Application Support 的漫游配置位置。`%APPDATA%` 为空时退回 `%USERPROFILE%\AppData\Roaming\Claude\configLibrary\`。Linux 按文档使用 `~/.config`，不用 `$XDG_CONFIG_HOME`。

`_meta.json` 记录当前启用的是哪一份配置，每一份配置是旁边的一个 `.json`。公开文档没有给出 `_meta.json` 的字段名。本工具使用应用内配置窗口实际写出的形状：`appliedId` 和 `entries`（`id`、`name`）。已经有一份被启用时，就地更新那份文件，不改选择。一份都没有时，创建名为 DeepRouter 的配置并选中它。其它已保存的配置仍留在列表里。API key 不会写进 `_meta.json`。

配置文件使用官方网关字段（[LLM gateway](https://claude.com/docs/third-party/claude-desktop/gateway)）：

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

`inferenceGatewayBaseUrl` 没有末尾 `/v1`。应用内示例就是这个形状（`https://llm-gateway.example.corp`）。Code 自己请求 `POST /v1/messages`。`GET /v1/models` 是可选的。只有一个模型 id 时写入 `inferenceModels`，除非文件里已经列了不止一个模型。没有模型列表时，只在 `modelDiscoveryEnabled` 还不存在的情况下把它设为 true。发现功能只展示应用认得出来的 Claude 模型 id。

两个文件都是模式 `0600`。托管配置会盖过这个目录：macOS 配置描述文件、Windows 机器策略，或必须由 root 拥有的 Linux 文件 `/etc/claude-desktop/managed-settings.json`。本工具不写这些位置。一个错误的 `/etc` 文件会让本地设置失效，而且它必须属于 root。

设置 `inferenceProvider` 会让应用进入第三方模式。除非登录时已经选了 Claude.ai，应用会以这个模式打开。本工具不设置 `disableDeploymentModeChooser`，所以 Claude.ai 登录仍然可选。如果之前选过 Claude.ai，需要在打开 Developer Mode 之后走 Developer → Configure Third-Party Inference，或者跳过 Anthropic 登录，然后退出并重新打开应用。

### Codex（CLI、桌面应用、IDE 扩展）

三者共用 `~/.codex/config.toml`。`CODEX_HOME` 可以改这个目录。Windows 默认是 `%USERPROFILE%\.codex\config.toml`。应用里：Settings → Configuration → Open config.toml。所以列表里不能把 Codex CLI 和 Codex 桌面分成两项，写一次就同时生效。

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

`model` 和 `model_provider` 留在第一个 `[table]` 之前的根表里，Codex 从那里读。注释和无关的表会保留。文件模式 `0600`。

key 放在 `experimental_bearer_token` 上，应用服务器和从图形界面启动的进程直接读 `config.toml`，不需要环境变量。这里不写 `env_key`：一旦写了，Codex 会在看 bearer token 之前返回 `Missing environment variable: DEEPROUTER_API_KEY`。

`~/.codex/auth.json`（或系统钥匙串，当 `cli_auth_credentials_store` 是 `keyring` 或 `auto`）是 ChatGPT / OpenAI API key 的登录缓存，不是自定义 provider 的凭据。本工具不把网关 key 写进去。

当前 Codex 走 Responses API，所以 `wire_api` 默认是 `responses`。旧版本可以传 `--codex-wire-api chat`。连通性测试始终调用 Chat Completions 和 Messages。配置过 Gemini CLI 时，还会用 `x-goog-api-key` 调用 `POST /v1beta/models/{model}:generateContent`。

### Grok Build

xAI 的编程 CLI，命令是 `grok`。用户配置是 `~/.grok/config.toml`（`$GROK_HOME/config.toml`；Windows 是 `%USERPROFILE%\.grok\config.toml`）。文档：[Settings](https://docs.x.ai/build/settings)、[Settings reference](https://docs.x.ai/build/settings/reference)、[Overview](https://docs.x.ai/build/overview)。

自定义模型是一个 `[model.<id>]` 表。`base_url` 是 OpenAI 风格，包含 `/v1`。`api_backend` 可以是 `chat_completions`、`responses` 或 `messages`。本工具写成 `chat_completions`，因为连通性测试和网关约定使用 `POST /v1/chat/completions`。

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

凭据顺序是 `api_key` > `env_key` > 已登录会话 > `XAI_API_KEY`。文档更建议用 `env_key`，避免把 key 写进文件。本工具仍然把 `api_key` 写进这个文件，并删掉 `[model.deeprouter]` 里的 `env_key`，这样从 Dock 或没有 shell 环境的方式启动时也能读到 key。文件模式 `0600`。段落 id 固定为 `deeprouter`；换模型只改 `model` 字段。其它 `[model.*]`、`[mcp_servers.*]`、注释，以及 `[models].web_search` 会保留。

项目里的 `.grok/config.toml` 只贡献 MCP、插件和权限规则，完整的用户配置只从 `~/.grok/config.toml` 读。本工具不写项目文件，也不写 `managed_config.toml`、`requirements.toml` 或 MDM。那些层的优先级更高，会盖过用户文件。

`GROK_MODELS_BASE_URL` 配合 `XAI_API_KEY` 也可以做环境变量方式的模型发现。图形界面或非 shell 启动看不到 shell 环境，所以不用它作为写入方式。改完配置后新开一个 Grok Build 会话，它才会重新读 `config.toml`。

### Gemini CLI

Gemini CLI 走 Gemini 的 `generateContent` 接口。`@google/genai` 把 `GOOGLE_GEMINI_BASE_URL` 当作基址，再接上 API 版本和路径，默认请求是 `{基址}/v1beta/models/{model}:generateContent`。所以这里写入的基址就是网关源站，例如 `https://deeprouter.top`，不带 `/v1`，也不带 `/v1beta`。鉴权用 `GEMINI_API_KEY`（请求头 `x-goog-api-key`）。Gemini CLI 要求这个基址是 HTTPS，只有 `localhost`、`127.0.0.1` 和 `[::1]` 可以用 HTTP。

这两个变量写进和其它环境变量同一处：macOS 和 Linux 是 shell 启动文件里的标记段，Windows 是用户环境 `HKCU\Environment`。`restore` 和 `uninstall` 会把它们恢复成第一次写入之前的值。`--apps gemini` 也会写这两个变量。`--no-shell` 不会跳过它们。

用户设置在 `~/.gemini/settings.json`（`GEMINI_CLI_HOME` 可以改 `.gemini` 的父目录）。Windows 默认是 `%USERPROFILE%\.gemini\settings.json`。本工具只合并两处，不覆盖文件里的其它字段：

- `security.auth.selectedType` 设为 `gemini-api-key`，这样启动时不再询问 Google 登录。
- 选了模型时写入 `model.name`。交互列表第 1 项、回车，或 `--gemini-model default` 会删掉 `model.name`，让 Gemini CLI 用自己的默认模型。`model` 里的其它字段保留。

还没安装时，向导和 `setup` 会提示官方命令 `npm install -g @google/gemini-cli`，文档在 https://geminicli.com/docs/get-started/installation/ 。配置仍会写上。项目里的 `.gemini/settings.json` 会盖过用户设置。系统设置在 Linux 的 `/etc/gemini-cli/settings.json`、Windows 的 `C:\ProgramData\gemini-cli\settings.json`、macOS 的 `/Library/Application Support/GeminiCli/settings.json`。文档：[配置](https://geminicli.com/docs/reference/configuration/)。

### Shell 启动文件

不带 `--apps` 的 `setup`，以及 stdin 不是终端时的无参数向导，在 macOS 和 Linux 上会在 `~/.zshrc`、`~/.bashrc` 或 `~/.config/fish/config.fish` 里写一段带标记的导出，供已经打开的终端 `source`。终端向导和 `--apps` 不给 Claude Code 或 Codex 写这段。选中 Gemini CLI 时例外：`GEMINI_API_KEY` 和 `GOOGLE_GEMINI_BASE_URL` 写进同一段，因为 Gemini CLI 从环境变量读 key 和基址。Windows 上不改 `.zshrc` 或 PowerShell profile。同一组变量写到当前用户的环境变量（注册表 `HKCU\Environment`，效果和 `setx` 一样，但没有 1024 字符截断）。新开的进程才会继承它们。`restore` 和 `uninstall` 会把这些变量恢复成第一次写入之前的值。传 `--rc-file` 时，Windows 也改为改那个启动文件，不写用户环境变量。Codex、Claude Desktop 和 Grok Build 不依赖这段导出。

```sh
# >>> deeprouter-setup >>>
export ANTHROPIC_BASE_URL="https://deeprouter.top"
export ANTHROPIC_AUTH_TOKEN="sk-your-key"
export ANTHROPIC_MODEL="claude-sonnet-4-6"
export DEEPROUTER_API_KEY="sk-your-key"
# <<< deeprouter-setup <<<
```

再次运行会替换这一段，不会再追加一份。

## 接不上这个网关的情况

登录 Claude.ai 的普通聊天不走自定义网关。Slack 里的 Claude Code，以及 Claude Code 云会话，都不属于网关部署：云会话里的网关变量不会生效。桌面网关配置生效时，Remote Control 不可用。这些界面继续使用 Anthropic。

托管策略优先于本地文件：Claude Desktop 的 MDM / `/etc/claude-desktop/managed-settings.json`，以及 Grok Build 的 `managed_config.toml`、`requirements.toml` 和 MDM。本工具不写这些位置。

## 常见问题

### API key 放在哪？

一次完整的、不带 `--apps` 的 setup 会把 key 写到这些地方：

- `~/.deeprouter/config.json`，模式 `0600`。每个程序的 key 不一样时，profile 里会多存一份覆盖值
- `~/.claude/settings.json` 的 `env.ANTHROPIC_AUTH_TOKEN`。Claude Code CLI 读它。Claude Desktop 的 Code 标签不拿它当网关凭据
- `~/.codex/config.toml` 里 `[model_providers.deeprouter]` 的 `experimental_bearer_token`。Codex CLI、桌面应用和 IDE 扩展读它。写完后会核对这个值和 profile 一致，并且 provider 没有 `env_key`。`auth.json` 不动
- Claude Desktop `configLibrary/` 里当前启用的文件，字段是 `inferenceGatewayApiKey`
- shell 启动文件里的 `ANTHROPIC_AUTH_TOKEN` 和 `DEEPROUTER_API_KEY`。Windows 上这两项在用户环境变量里，不在启动文件里

只选了部分程序时，key 只进入那些程序的文件和 profile。`GET /v1/models` 和 chat completion 用 `Authorization: Bearer`。messages 检查还会带 `x-api-key` 和 `anthropic-version`。不跟随重定向。

`deeprouter status` 会打印 `claude` 和 `codex` 是否在 `PATH` 上，以及各自 `--version` 的第一行。交互式配置在其中某个缺失时询问 `Install missing CLIs with npm? [y/N]`。`setup --install-clis` 对缺失的那个执行 `npm install -g @anthropic-ai/claude-code @openai/codex`。没有 `npm` 时改为打印官方安装页，不安装。

已有 key 时，交互提示形如 `API key [sk-n…Mc04]:`（每个程序前面会加上程序名）。回车保留，输入则替换。stdin 不是终端时不提示：使用 `--api-key`、`DEEPROUTER_API_KEY` 或已经保存的 key。

`~/.deeprouter/backups` 里是每次修改前的文件副本，所以里面可能有曾经写在 `settings.json` 或 shell rc 里的 key。`deeprouter uninstall --purge` 会删掉这个目录。

### 怎么撤回？

`deeprouter restore` 写回本工具第一次改每个文件之前的副本。本工具新建的文件会被删掉。Profile 保留，可以再跑 `setup`。

`deeprouter restore --list` 列出之后的快照。`deeprouter restore --id latest` 只撤销最近一次修改。

`deeprouter uninstall` 做同样的还原。加上 `--purge` 会删除 `~/.deeprouter`，包括保存的 profile 和备份。

### 能换一个网关吗？

可以。任何提供 `GET /v1/models`、`POST /v1/chat/completions` 和 `POST /v1/messages` 的基址都可以。配置了 Gemini CLI 时，同一基址还要提供 `POST /v1beta/models/{model}:generateContent`。

```sh
deeprouter setup --base-url https://gateway.example.com --api-key "$GATEWAY_KEY"
```

`test` 会报告 HTTP 状态和一句短提示：`401` key 不对，`403` 配额或权限，`404` 模型或路径，`503` 没有可用通道。

### Windows 能用吗？

可以。发布包含 `deeprouter-windows-amd64.exe` 和 `deeprouter-windows-arm64.exe`，校验和在 `checksums.txt` 里。用上面的 PowerShell 或 cmd 命令安装。交互式提问走 Windows 控制台：输入 API key 时每个字符显示一个 `*`（控制台逐字读取，不用 `stty`，也不打开 `/dev/tty`）。

Claude Code 写 `%USERPROFILE%\.claude\settings.json`，Codex 写 `%USERPROFILE%\.codex\config.toml`（`CODEX_HOME` 可以改目录），Claude Desktop 的第三方配置写 `%APPDATA%\Claude\configLibrary\`，Grok Build 写 `%USERPROFILE%\.grok\config.toml`（`GROK_HOME` 可以改目录）。环境变量写入用户环境 `HKCU\Environment`，不改 shell 启动文件。

## 为什么用 Go

工具是一个静态二进制，模块没有第三方依赖。改别人的 JSON、TOML 和 shell 启动文件必须幂等，并且必须留下文件的其余部分，这在 Go 里比单独用 shell 脚本更好测。`install.sh` 和 `install.ps1` 是两个单文件入口：在检出的仓库里编译那个二进制，或者在你从网上运行时下载发布包。macOS 和 Linux 用 `install.sh`。Windows 用 `install.ps1`，用户环境变量写注册表而不是 shell 启动文件。

## 开发

```sh
go test ./...
go build -o deeprouter .
```

CI 在 Linux、macOS 和 Windows 上跑测试，在 Linux 和 macOS 上跑 `install.sh`，在 Windows 上跑 `install.ps1`，并在 Linux 上交叉编译 `windows/amd64` 和 `windows/arm64`。核心覆盖是配置写入（Claude 设置、Codex TOML、Claude Desktop 的 `configLibrary`、Grok Build 的 `config.toml`、shell rc、Windows 用户环境变量、备份），以及针对本地假网关的 install、setup、test、switch、restore 端到端流程。

推送 `v*` 标签会跑 `.github/workflows/release.yml`。工作流用 `CGO_ENABLED=0` 为 darwin/amd64、darwin/arm64、linux/amd64、linux/arm64、windows/amd64、windows/arm64 构建静态二进制。Windows 产物是 `.exe`，和其他平台一样是普通文件，不是 zip。SHA256 写到 `checksums.txt`，并发布 GitHub Release。二进制里的版本是去掉前导 `v` 的标签（`v0.1.0` 变成 `0.1.0`），通过 `-X main.version` 注入。

## 许可证

MIT。见 [LICENSE](LICENSE)。

# deeprouter-setup

一条命令，把兼容 OpenAI 与 Anthropic 的 API 网关接到你选定的编程工具上。

![deeprouter-setup configuring Claude Code and Codex](docs/demo.gif)

发布到 Product Hunt 和 Show HN 之前，把 `docs/demo.gif` 换成一段短录屏。

## 安装

```sh
curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.sh | sh
```

二进制装到 `~/.local/bin`。这个目录不在 `PATH` 上时，安装程序会打印 `export` 行，并把它写进 shell 启动文件（`~/.zshrc`、`~/.bashrc` 或 fish 配置）。加 `--no-path` 可以跳过这一步。

安装成功后，如果当前进程有控制终端，会立刻用刚装好的绝对路径启动交互式配置，并从 `/dev/tty` 读输入。`curl | sh` 的标准输入是安装脚本本身，所以向导不能读那条管道。没有控制终端时（CI、其他非交互会话）不启动，改为打印下一步要运行的绝对路径。设置 `DEEPROUTER_NO_SETUP=1` 同样跳过自动启动。这一次启动不依赖 `PATH` 是否已经刷新；之后新开的终端才会用上写进启动文件的 `PATH`。

从仓库安装：

```sh
git clone https://github.com/zfg5118/deeprouter-setup.git
cd deeprouter-setup
sh install.sh
```

`install.sh` 是 POSIX `sh`。在仓库旁边时，它用 Go 1.22 或更新版本编译一个静态二进制。二进制里的版本号是 `git describe --tags`；没有标签时是 `dev`。从网上管道安装时，它从 `https://github.com/zfg5118/deeprouter-setup/releases/latest/download/` 下载普通的发布二进制（不是压缩包），并用同一路径下的 `checksums.txt` 校验。这一步不调用 GitHub API。文件名是 `deeprouter-setup-darwin-amd64`、`deeprouter-setup-darwin-arm64`、`deeprouter-setup-linux-amd64`、`deeprouter-setup-linux-arm64`。`DEEPROUTER_SETUP_VERSION` 不带前导 `v`（例如 `0.1.1`）会固定到 `releases/download/v0.1.1/`。Fork 之后设置 `DEEPROUTER_SETUP_REPO=owner/name`。安装完成后没有运行时依赖。

## 快速开始

```sh
deeprouter-setup
```

在终端里不带参数运行，会进入交互式配置。`install.sh` 在有控制终端时会在装完后直接进入这一步。程序只改你勾选的那些工具，其余文件不动。

1. 先选择要配置的程序，可以多选。直接回车等于 `1,2,3`：Claude Code、Claude Desktop、Codex。Codex 的 CLI 和桌面应用共用同一个文件，所以列表里只有一项。Grok Build 要单独选。Gemini CLI 会出现在列表里，但不能写入，原因见下文。
2. 选择 official-key 分组。回车保持 Officially。
3. 网关地址。回车保持 `https://deeprouter.top`。
4. **对每一个选中的程序单独**询问 API key 和模型。已有 key 时提示类似 `Claude Code API key [sk-n…Mc04]:`。输入被隐藏。回车保留当前 key，输入新值就替换它。模型来自 `GET /v1/models`，按程序过滤：Claude 用包含 `claude` 的 id，Codex 用包含 `gpt` 的 id，Grok Build 用包含 `grok` 的 id。回车接受建议的 id。同一个 key 只拉一次模型列表。
5. 只把选中程序的配置写盘，然后做连通性测试。终端向导不改 shell 启动文件。macOS 上的图形界面本来也不会读 `~/.zshrc`。
6. 如果 `claude` 或 `codex` 没安装，会先问再装（`y/N`）。不回答 `y` 就不会安装。

非交互（脚本、stdin 不是终端）用 `setup` 和标志，不走上面的问答：

```sh
export DEEPROUTER_API_KEY=sk-your-key
deeprouter-setup setup --group officially
deeprouter-setup setup --apps grok --grok-model grok-4 --grok-key "$GROK_KEY"
deeprouter-setup test
```

不带 `--apps` 的 `setup` 仍会写入 Claude Code、Claude Desktop、Codex 和 shell 启动文件，和以前的脚本兼容。`--apps` 只写列出的程序，并且不改 shell。`--claude-only` 和 `--codex-only` 不能和 `--apps` 一起用。

## 命令

| 命令 | 作用 |
| --- | --- |
| `setup` | 为一个 profile 写入所选程序的配置 |
| `test` | `GET /v1/models`、一次单 token 的 chat completion、一次单 token 的 messages。打印 HTTP 状态、首字节时间和总时间 |
| `models` | 列出网关上的模型 id |
| `switch` | 切换 profile，或改某一个程序的模型 |
| `profile list\|add\|use\|remove` | 管理具名 profile。一个 profile 保存一把 API key |
| `groups` | 列出建议的 official-key 分组 |
| `status` | 当前 profile、每个工具是否已接上、`claude` 和 `codex` 是否已安装（含版本） |
| `restore` | 从备份还原被改过的文件 |
| `uninstall` | 还原这些文件。`--purge` 同时删除 `~/.deeprouter` |
| `version` | 打印版本 |

`deeprouter-setup <command> --help` 可以看标志。`test` 和 `models` 接受 `--json`。

退出码：`0` 成功，`1` 网关检查或写文件失败，`2` 命令行不正确。

## 非交互标志

```sh
deeprouter-setup setup \
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
| `--apps LIST` | `claude-code`、`claude-desktop`、`codex`、`grok`，逗号或空格分隔。含 `gemini` 时命令失败，并且什么都不写 |
| `--api-key` / `--api-key-stdin` / `DEEPROUTER_API_KEY` | 各程序共用的 key。某个程序没单独给 key 时用它 |
| `--claude-code-key`、`--claude-desktop-key`、`--codex-key`、`--grok-key` | 只写入对应程序。省略时用共用 key |
| `--claude-model`、`--claude-desktop-model`、`--codex-model`、`--grok-model` | 对应程序的模型 id。省略时从 `/v1/models` 里挑一个。Grok 的列表里没有包含 `grok` 的 id 时，必须传 `--grok-model` |
| `--no-shell` | 不改 shell 启动文件。`--apps` 本来就不会改 |
| `--claude-only` | 写 Claude Code 和 Claude Desktop，不写 Codex。不能和 `--apps` 一起用 |
| `--codex-only` | 只写 Codex，不写 Claude Code 和 Claude Desktop |

`--api-key` 会出现在进程列表里。脚本里更适合用 `DEEPROUTER_API_KEY` 或 `--api-key-stdin`。

`switch` 只更新上次实际写入过的那些程序。一个只配过 Grok Build 的 profile，切换时不会改 Claude Code 或 Codex。模型可以用 `--claude-model`、`--claude-desktop-model`、`--codex-model`、`--grok-model` 单独改。

## 分组和 profile

分组由 profile 上的 API key 决定。可以先从这些 official-key 分组开始：

| Profile | 标签 | 用途 |
| --- | --- | --- |
| `openai` | OpenAI | OpenAI 官方 key |
| `azure` | Azure | Azure OpenAI key |
| `aws` | AWS | Amazon Bedrock key |
| `vertex` | Vertex | Google Vertex key |
| `officially` | Officially | 官方供应商 key |

```sh
deeprouter-setup setup --group officially --api-key "$DEEPROUTER_API_KEY"
deeprouter-setup profile add azure --label Azure --api-key "$AZURE_KEY"
deeprouter-setup switch --group azure
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
| Windows | `%LOCALAPPDATA%\Claude-3p\configLibrary/` |
| Linux | `~/.config/Claude-3p/configLibrary/` |

Windows 旧版本用过 `%APPDATA%\Claude-3p\`。现在的版本升级时会把目录迁到 `%LOCALAPPDATA%`。本工具写当前这个路径。Linux 按文档使用 `~/.config`，不用 `$XDG_CONFIG_HOME`。

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

当前 Codex 走 Responses API，所以 `wire_api` 默认是 `responses`。旧版本可以传 `--codex-wire-api chat`。连通性测试始终调用 Chat Completions 和 Messages，这是兼容网关公开的那一对接口。

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

不写入任何 Gemini 配置。

Gemini CLI 的用户设置在 `~/.gemini/settings.json`（`GEMINI_CLI_HOME` 可以改 `.gemini` 的父目录）。项目 `.gemini/settings.json` 覆盖用户设置。系统设置在 Linux 的 `/etc/gemini-cli/settings.json`、Windows 的 `C:\ProgramData\gemini-cli\settings.json`、macOS 的 `/Library/Application Support/GeminiCli/settings.json`。文档：[配置](https://geminicli.com/docs/reference/configuration/)。

自定义基址只有环境变量 `GOOGLE_GEMINI_BASE_URL`（除 localhost、`127.0.0.1`、`[::1]` 外必须是 HTTPS）。它传给 Google GenAI SDK 的 `httpOptions.baseUrl`，请求仍是 Gemini 的 `generateContent`，默认鉴权头是 `x-goog-api-key`。设置里没有已发布的 base URL 字段。本网关提供的是 OpenAI `/v1` 和 Anthropic `/v1/messages`，不是 Gemini 协议，所以把这个变量指到网关并不能用。

`.env` 从当前目录向上找，先看受信任目录里的 `.gemini/.env`，再看 `.env`，最后才是 `~/.gemini/.env` 和 `~/.env`。找到的第一个文件生效，项目里的 `.env` 会挡住家目录那份。不受信任的工作区只加载 `GEMINI_API_KEY`、`GOOGLE_API_KEY`、`GOOGLE_CLOUD_PROJECT`、`GOOGLE_CLOUD_LOCATION`，其中没有 `GOOGLE_GEMINI_BASE_URL`。

交互式列表里可以选 Gemini CLI，工具会说明原因并继续配置其它选中的程序。`setup --apps` 里只要出现 `gemini`，整个命令以退出码 2 结束，一个文件都不写。

### Shell 启动文件

终端向导和 `--apps` 都不改 shell。不带 `--apps` 的 `setup`，以及 stdin 不是终端时的无参数向导，仍会在 `~/.zshrc`、`~/.bashrc`、`~/.config/fish/config.fish` 或 PowerShell profile 里写一段带标记的导出，供已经打开的终端 `source`。Codex、Claude Desktop 和 Grok Build 不依赖这段导出。

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

Gemini CLI 不能用，见上一节。

托管策略优先于本地文件：Claude Desktop 的 MDM / `/etc/claude-desktop/managed-settings.json`，以及 Grok Build 的 `managed_config.toml`、`requirements.toml` 和 MDM。本工具不写这些位置。

## 常见问题

### API key 放在哪？

一次完整的、不带 `--apps` 的 setup 会把 key 写到这些地方：

- `~/.deeprouter/config.json`，模式 `0600`。每个程序的 key 不一样时，profile 里会多存一份覆盖值
- `~/.claude/settings.json` 的 `env.ANTHROPIC_AUTH_TOKEN`。Claude Code CLI 读它。Claude Desktop 的 Code 标签不拿它当网关凭据
- `~/.codex/config.toml` 里 `[model_providers.deeprouter]` 的 `experimental_bearer_token`。Codex CLI、桌面应用和 IDE 扩展读它。写完后会核对这个值和 profile 一致，并且 provider 没有 `env_key`。`auth.json` 不动
- Claude Desktop `configLibrary/` 里当前启用的文件，字段是 `inferenceGatewayApiKey`
- shell 启动文件里的 `ANTHROPIC_AUTH_TOKEN` 和 `DEEPROUTER_API_KEY`

只选了部分程序时，key 只进入那些程序的文件和 profile。`GET /v1/models` 和 chat completion 用 `Authorization: Bearer`。messages 检查还会带 `x-api-key` 和 `anthropic-version`。不跟随重定向。

`deeprouter-setup status` 会打印 `claude` 和 `codex` 是否在 `PATH` 上，以及各自 `--version` 的第一行。交互式配置在其中某个缺失时询问 `Install missing CLIs with npm? [y/N]`。`setup --install-clis` 对缺失的那个执行 `npm install -g @anthropic-ai/claude-code @openai/codex`。没有 `npm` 时改为打印官方安装页，不安装。

已有 key 时，交互提示形如 `API key [sk-n…Mc04]:`（每个程序前面会加上程序名）。回车保留，输入则替换。stdin 不是终端时不提示：使用 `--api-key`、`DEEPROUTER_API_KEY` 或已经保存的 key。

`~/.deeprouter/backups` 里是每次修改前的文件副本，所以里面可能有曾经写在 `settings.json` 或 shell rc 里的 key。`deeprouter-setup uninstall --purge` 会删掉这个目录。

### 怎么撤回？

`deeprouter-setup restore` 写回本工具第一次改每个文件之前的副本。本工具新建的文件会被删掉。Profile 保留，可以再跑 `setup`。

`deeprouter-setup restore --list` 列出之后的快照。`deeprouter-setup restore --id latest` 只撤销最近一次修改。

`deeprouter-setup uninstall` 做同样的还原。加上 `--purge` 会删除 `~/.deeprouter`，包括保存的 profile 和备份。

### 能换一个网关吗？

可以。任何提供 `GET /v1/models`、`POST /v1/chat/completions` 和 `POST /v1/messages` 的基址都可以。

```sh
deeprouter-setup setup --base-url https://gateway.example.com --api-key "$GATEWAY_KEY"
```

`test` 会报告 HTTP 状态和一句短提示：`401` key 不对，`403` 配额或权限，`404` 模型或路径，`503` 没有可用通道。

### Windows 能用吗？

配置写入在 Windows 上可以运行。Claude Code 设置在 `%USERPROFILE%\.claude\settings.json`，Codex 在 `%USERPROFILE%\.codex\config.toml`，Claude Desktop 的本地库在 `%LOCALAPPDATA%\Claude-3p\configLibrary\`，Grok Build 在 `%USERPROFILE%\.grok\config.toml`，shell 段写到 PowerShell profile。`install.sh` 下载 macOS 和 Linux 的发布二进制。Windows 上用 `go build -o deeprouter-setup.exe .` 编译。

## 为什么用 Go

工具是一个静态二进制，模块没有第三方依赖。改别人的 JSON、TOML 和 shell 启动文件必须幂等，并且必须留下文件的其余部分，这在 Go 里比单独用 shell 脚本更好测。`install.sh` 是单文件入口：在检出的仓库里编译那个二进制，或者在你从网上管道运行时下载发布包。macOS 和 Linux 是安装脚本的目标；Windows 是同一份 Go 代码，shell 换成 PowerShell profile。

## 开发

```sh
go test ./...
go build -o deeprouter-setup .
```

CI 在 Linux、macOS 和 Windows 上跑测试，并在 Linux 和 macOS 上跑 `install.sh`。核心覆盖是配置写入（Claude 设置、Codex TOML、Claude Desktop 的 `configLibrary`、Grok Build 的 `config.toml`、shell rc、备份），以及针对本地假网关的 install、setup、test、switch、restore 端到端流程。

推送 `v*` 标签会跑 `.github/workflows/release.yml`。工作流用 `CGO_ENABLED=0` 为 darwin/amd64、darwin/arm64、linux/amd64、linux/arm64 构建静态二进制，把 SHA256 写到 `checksums.txt`，并发布 GitHub Release。二进制里的版本是去掉前导 `v` 的标签（`v0.1.0` 变成 `0.1.0`），通过 `-X main.version` 注入。

## 许可证

MIT。见 [LICENSE](LICENSE)。

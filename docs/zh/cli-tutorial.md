# DeepRouter CLI 教程

deeprouter 把 Claude Code、Codex / ChatGPT 和 Gemini CLI 接到 OpenAI、Anthropic 兼容的网关。默认网关是 `https://deeprouter.top`。它只改你选中的程序配置，API key 保存在本机 profile 里。

## 安装

macOS 和 Linux：

```sh
curl -fsSL https://deeprouter.top/install.sh | sh
```

二进制装到 `~/.local/bin/deeprouter`（可用 `PREFIX` 改目录）。

Windows（PowerShell）：

```powershell
irm https://deeprouter.top/install.ps1 | iex
```

cmd 里没有 `irm`，用：

```bat
powershell -ExecutionPolicy Bypass -c "irm https://deeprouter.top/install.ps1 | iex"
```

二进制装到 `%LOCALAPPDATA%\Programs\deeprouter`。不要在 Windows 上跑 `install.sh`。

`https://deeprouter.top` 上的安装脚本先从镜像下载，失败再回到 GitHub。GitHub 上的脚本相反：先试 GitHub，失败再试 `https://deeprouter.top`。第一个地址的每个文件等 45 秒，第二个地址等 120 秒。两条路都会校验同一处的 `checksums.txt`，并打印实际用的地址。GitHub 入口（私有仓库 `WiltFan/DeepRouter`，需登录 GitHub 且有权限；否则请用上面的 `https://deeprouter.top` 镜像）：

```sh
curl -fsSL https://raw.githubusercontent.com/WiltFan/DeepRouter/main/install.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/WiltFan/DeepRouter/main/install.ps1 | iex
```

`DEEPROUTER_DOWNLOAD_BASE` 指定先试哪个地址。镜像路径是 `releases/<版本或 latest>/<文件>`，不是 GitHub 的 `releases/download/`。

```sh
DEEPROUTER_DOWNLOAD_BASE=https://deeprouter.top curl -fsSL https://raw.githubusercontent.com/WiltFan/DeepRouter/main/install.sh | sh
```

```powershell
$env:DEEPROUTER_DOWNLOAD_BASE = 'https://deeprouter.top'
irm https://raw.githubusercontent.com/WiltFan/DeepRouter/main/install.ps1 | iex
```

镜像上的 `install.sh` / `install.ps1` 已经内置先用 `https://deeprouter.top`，一般不用再设这个变量。

有控制终端时，装完会直接进入交互配置。不想自动进入时设置 `DEEPROUTER_NO_SETUP=1`。

## 获取 API key

在 [deeprouter.top](https://deeprouter.top) 创建 API key。下面的命令只用占位符 `sk-your-key`。不要把真实 key 写进仓库、截图或会进进程列表的命令。脚本里用环境变量或 `--api-key-stdin`，不要用 `--api-key`（它会出现在进程列表里）。

```sh
export DEEPROUTER_API_KEY=sk-your-key
```

```powershell
$env:DEEPROUTER_API_KEY = 'sk-your-key'
```

## 配置

终端里不带参数运行 `deeprouter` 会进入向导。界面默认是简体中文，`setup`、`test` 等命令的输出和报错也一样。只有 `LC_ALL`（为空时看 `LANG`）明确是英文（例如 `en_US.UTF-8`）时才用英文；`C`、`POSIX` 和空值仍是中文。`--lang zh` 或 `--lang en` 可以直接指定。标准输入不是终端时（`deeprouter </dev/null` 或管道），向导打印 `请在终端中运行 deeprouter。` 和 `setup` 的用法，不写文件，退出码是 2。第一步选择程序。提示是 `程序 [1]:`，多选示例是 `1,2`。直接回车配置 Codex / ChatGPT。

然后选择平台。DeepRouter、WolfAI 和 LinkMesh 支持所有客户端，所以每一份列表里都有它们。官方接口只在选中的每个程序都能使用时出现。自定义网关地址始终在最后。只选 Codex / ChatGPT 时，列表是：

1. DeepRouter（默认，`https://deeprouter.top`）
2. WolfAI（`https://wolfai.top`）
3. LinkMesh（`https://www.linkmesh.org`）
4. OpenAI（`https://api.openai.com`）
5. xAI Grok（`https://api.x.ai`）
6. 自定义网关地址

只选 Claude Code 或 Claude Desktop 时，第 4 项是 Anthropic/Claude（`https://api.anthropic.com`），下一项就是自定义网关地址。只选 Gemini CLI 时，第 4 项是 Google Gemini（`https://generativelanguage.googleapis.com`）。只选 Grok Build 时，第 4 项是 xAI Grok。选了多个程序时，官方接口必须对每一个都可用才会出现；三家网关仍然都在，自定义地址仍在最后。

直接回车选择 DeepRouter。`DEEPROUTER_PROVIDER=wolfai`（或 `openai`、`anthropic`、`gemini`、`xai`）会在该平台出现在列表里时把它标成默认项。OpenAI 和 xAI 写入 Codex / ChatGPT（`wire_api = "responses"`，`base_url` 带 `/v1`），Anthropic/Claude 写入 Claude Code 和 Claude Desktop（`ANTHROPIC_BASE_URL` 不带 `/v1`），Google Gemini 写入 Gemini CLI（基址是源站，CLI 自己补 `/v1beta`）。这些预设会先确认令牌，不获取模型列表，模型用客户端默认。已有的 Codex 模型行会保留。只有最后一项自定义地址才会接着问网关地址。

接着为每个程序问令牌。什么都不输入就回车时，打印 `令牌不能为空，请重新输入。` 再问一次。网关拒绝令牌（401、403，或返回内容说明密钥无效的 HTTP 400）时，打印网关给的原因，然后重新问。这次重新输入时按 Ctrl-D，退出码是 1，打印 `令牌被拒绝。没有写入配置。`。

模型列表的第 1 项，在 Codex / ChatGPT 已经有模型时是 `保留已有模型 gpt-5`，回车保留这一行。没有已有模型时，第 1 项是 `不指定模型（使用客户端默认）`，回车就选它。Claude Code、Claude Desktop 和 Gemini CLI 的第 1 项仍是不指定模型，并会删掉以前写过的模型行。Grok Build 没有这一项：列表从检测通过的模型开始，第 1 项是最新的那个，直接回车就选它，必须写入一个具体模型。其余每一项都是同时探测后有应答的模型，并标着 `（已通过测试）`。没有应答的会打印原因，例如 `已跳过 gpt-5.5：503 没有可用渠道。……`，不出现在列表里。一个都没有应答时，其它程序的列表只剩第 1 项，可以手动输入模型 id。Grok Build 这时没有编号项，必须输入模型 id，直接回车不会跳过。输入 `n` 这类既不是编号也不是模型 id 的内容会再问一次。

写入前会列出将要修改的文件，并说明修改前会留下备份，撤销请运行 `deeprouter restore`。然后直接写入，不再询问 `继续写入？[Y/n]`。`deeprouter setup` 同样不询问。按 Ctrl-D 在写入之前取消，退出码是 2。令牌已经被拒绝之后再按 Ctrl-D，退出码是 1。网关的连通性测试没通过时，可以选 `1) 换一个模型`，或 `2) 撤销刚才的写入`（默认）。官方直连会先确认令牌，不做这步模型列表探测。令牌被拒绝（401、403，或密钥无效的 HTTP 400）会重新询问令牌。连不上（没有响应或超时）时说明需要能访问官方接口的网络（从中国大陆通常是海外网络），并询问 `不检查令牌，继续写入配置？[y/N]`。回答否时提示检查网络或代理后重新运行，不提 `--skip-verify`。服务器有响应时不是网络问题，不会提示需要海外网络。

非交互用 `setup`。不带 `--apps` 时写该平台的默认程序，和向导直接回车一样。`--apps` 只写列出的程序。网关在写入之前会先确认令牌，拒绝时打印原因和 `没有写入配置。`，退出码是 1。官方预设会确认令牌，不提供模型列表。非交互连不上时需要 `--skip-verify`。令牌被拒绝仍然失败，包括 Gemini 和 xAI 的 HTTP 400。服务器有响应时不是网络问题。

```sh
deeprouter setup --provider wolfai
deeprouter setup --provider linkmesh --base-url https://example.com
deeprouter setup --provider openai
deeprouter setup --provider anthropic
deeprouter setup --provider gemini
deeprouter setup --provider xai --apps codex,grok
```

`--provider` 取 `deeprouter`、`wolfai`、`linkmesh`、`openai`、`anthropic`、`gemini` 或 `xai`。`--base-url` 只改地址，不改平台 id。环境变量 `DEEPROUTER_PROVIDER` 同样可选。`install.sh` / `install.ps1` 接受 `--provider`，并会把 `DEEPROUTER_PROVIDER` 传给后面的配置。官方预设会确认令牌，不获取模型列表。非交互连不上时需要 `--skip-verify`。不带 `--apps` 时，Anthropic/Claude 写 Claude Code 和 Claude Desktop（`ANTHROPIC_API_KEY`），Google Gemini 写 Gemini CLI，其它平台写 Codex / ChatGPT。网关上的 Claude Code 仍写 `ANTHROPIC_AUTH_TOKEN`。

写入 Codex 的 `model_provider` 和 `[model_providers.<id>]`，以及 Grok Build 的 `[model.<id>]`，用的是所选平台的 id，例如 `model_provider = "wolfai"`。再次配置会删掉以前写过的 `deeprouter`、`wolfai`、`linkmesh`、`openai-api`、`anthropic`、`gemini`、`xai`、`custom` 段。`--provider openai` 写的是 `openai-api`，不会动 Codex 自带的 `[model_providers.openai]`。其它表不动。

每个平台的令牌不共用。向导会点名站点，例如 `请输入 WolfAI 的令牌（在 wolfai.top 创建），用于 Codex / ChatGPT：`。已经保存的 key 只在同一个平台上复用；`DEEPROUTER_API_KEY` 只属于 DeepRouter。LinkMesh 不是 new-api，不会请求 `/api/status`。

LinkMesh 是 sub2api。不带 key 探测时，`POST /v1/chat/completions`、`POST /v1/responses`、`POST /v1/messages` 和 `POST /v1beta/models/{model}:generateContent` 都是 401（路由存在）。`POST /v1/models/{model}:generateContent` 是 404，Gemini 不用这条路径。因此 Codex 的 `wire_api` 是 `responses`，`base_url` 是 `https://www.linkmesh.org/v1`；Claude Code 的 `ANTHROPIC_BASE_URL` 是 `https://www.linkmesh.org`（不带 `/v1`）。这五个程序都会写入。路由不存在的程序会被跳过并打印原因。

模型 id 以 `deeprouter models` 的结果为准；下面的 id 只是示例。省略模型标志或传 `default` 时，setup 不写模型，但 Grok Build 除外：没传 `--grok-model` 时自动写入检测通过的最新模型；一个都没通过就以退出码 1 结束，说明原因，不写配置。`--grok-model default` 不会接受。Codex / ChatGPT 会保留已有的根表 `model` 行；其它程序会删掉以前写过的模型行。连通性检查仍会用同家族里能应答的对话模型发测试请求，但不保存这个 id。Grok Build 会保存自动选中的那个 id。传了具体模型 id 时，setup 先测试这个模型；没有应答就以退出码 1 结束，不写任何文件。只有明确指定的模型 id 会替换 Codex 的 `model` 行。

### Claude Code

还没安装时：

```sh
curl -fsSL https://claude.ai/install.sh | bash
```

Windows：`irm https://claude.ai/install.ps1 | iex`。

```sh
deeprouter setup --apps claude-code --claude-model claude-sonnet-5-5
```

写入 `~/.claude/settings.json`（Windows：`%USERPROFILE%\.claude\settings.json`）。`ANTHROPIC_BASE_URL` 不带末尾 `/v1`。

### Codex / ChatGPT

Codex CLI 和 ChatGPT 桌面应用共用一份配置。还没安装 CLI 时：

```sh
npm install -g @openai/codex
```

```sh
deeprouter setup --apps codex --codex-model gpt-6.1-sol
```

写入 `~/.codex/config.toml`（`CODEX_HOME` 可改目录；Windows 默认 `%USERPROFILE%\.codex\config.toml`）。`base_url` 带 `/v1`，key 在 `experimental_bearer_token`。`wire_api` 默认 `responses`。表名是平台 id：默认 `[model_providers.deeprouter]`，`--provider wolfai` 则是 `[model_providers.wolfai]`，根上的 `model_provider` 写成同一个 id。配完后退出并重新打开 Codex / ChatGPT。

### Gemini CLI

```sh
npm install -g @google/gemini-cli
```

macOS 和 Linux 也可以 `brew install gemini-cli`。

```sh
deeprouter setup --apps gemini --gemini-model gemini-2.5-pro
```

`GEMINI_API_KEY` 和 `GOOGLE_GEMINI_BASE_URL` 写入 shell 启动文件（Windows 是用户环境变量）。基址是网关源站，例如 `https://deeprouter.top`，不带 `/v1`。`~/.gemini/settings.json` 里会设置 `security.auth.selectedType = gemini-api-key`，选了模型时写入 `model.name`，没选时删掉它。新开一个终端后再运行 `gemini`。

## 测试连通性

```sh
deeprouter test
```

检查当前 profile 里配置过的程序：先 `GET /v1/models`，然后 Codex / ChatGPT 是 `POST /v1/responses`（`wire_api = "chat"` 时是 `POST /v1/chat/completions`），Claude Code 是 `POST /v1/messages`，Grok Build 是 `POST /v1/chat/completions`，Gemini CLI 是 `POST /v1beta/models/{model}:generateContent`。打印 HTTP 状态、首字节时间和总时间。`--json` 输出 JSON。没选模型时，用同家族里能应答的对话模型测试，并标明 `（仅用于测试，未写入配置）`。网关列表里没有这个家族的模型时，这项显示为 `已跳过`，不算失败。

## 升级

再执行一次上面的安装命令。它下载当前发布的二进制，并用 `checksums.txt` 校验。查看版本：

```sh
deeprouter version
```

固定某一个版本时，`DEEPROUTER_SETUP_VERSION` 不带前导 `v`（例如 `0.3.4`）。URL 里仍带 `v`。

## 常见问题

### Windows 校验和失败

PowerShell 5.1 在 0.3.3 之前会把 `checksums.txt` 读错，校验对不上。0.3.3 及以后已修复。请用上面的安装命令重装，不要沿用旧的 `install.ps1`。下载到的内容如果是网页或空文件，安装会失败并改试下一个地址，不会装一个对不上的二进制。只有 `checksums.txt` 返回 404（这一版没发布校验文件）才跳过校验。

### Codex 提示 disable_response_storage is ignored

当前 Codex 已删除 `disable_response_storage`，配置里留下这一行时会提示它被忽略。deeprouter 不写入这个键。从 0.3.5 起，再执行一次：

```sh
deeprouter setup --apps codex
```

会从根表和 `[model_providers.deeprouter]` 里自动删掉 `disable_response_storage`。其他表里的同名键保持不动。也可以手动打开 `~/.codex/config.toml`（Windows：`%USERPROFILE%\.codex\config.toml`），删掉根表和 `[model_providers.deeprouter]` 里的那一行。

### npm 很慢

系统里没有 npm 时，向导不问安装，也不问镜像，只说明如何安装 Node.js（自带 npm），配置仍然成功。有 npm 时，向导先问 `用 npm 安装缺少的命令行工具？`。回答是之后，如果 npmjs.org 短时间连不上，或语言 / 时区像中国大陆，会提到你可以自己执行：

```sh
npm config set registry https://registry.npmmirror.com
```

提问之前会先打印 `npm install -g ...`。然后问 `这一次安装使用 https://registry.npmmirror.com ？`。两个问题在任何环境都默认否（`[y/N]`）。两个都回答是才会执行 `npm install -g --registry https://registry.npmmirror.com ...`；镜像问题回答否时用默认的 npm 源。安装问题回答否或直接回车时什么都不装，也不再问镜像。程序不会替你改已保存的 npm 配置。`--install-clis` 不会出现这个提问。

### Codex 返回 400

`deeprouter test` 在 4xx 时会打印 `HTTP 400。网关返回：……`，冒号后面是网关的错误信息（key 已打码）。Codex 自己走 Responses API（`wire_api = "responses"`），测试打的也是 `POST /v1/responses`，所以测试里看到的 400 就是 Codex 会遇到的那一个。

先看那句错误。模型不对时运行 `deeprouter models`，再用网关列出的 gpt 对话模型重配：

```sh
deeprouter setup --apps codex --codex-model gpt-6.1-sol
```

或者不传 `--codex-model`，删掉模型行，让 Codex 用自己的默认模型。

当前 Codex 会拒绝 `wire_api = "chat"`，那是配置解析错误，不是这个 HTTP 400。不要把 `wire_api` 改成 `chat`。

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

`https://deeprouter.top` 上的安装脚本先从镜像下载，失败再回到 GitHub。GitHub 上的脚本相反：先试 GitHub，失败再试 `https://deeprouter.top`。两条路都会校验同一处的 `checksums.txt`，并打印实际用的地址。GitHub 入口：

```sh
curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex
```

`DEEPROUTER_DOWNLOAD_BASE` 指定先试哪个地址。镜像路径是 `releases/<版本或 latest>/<文件>`，不是 GitHub 的 `releases/download/`。

```sh
DEEPROUTER_DOWNLOAD_BASE=https://deeprouter.top curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.sh | sh
```

```powershell
$env:DEEPROUTER_DOWNLOAD_BASE = 'https://deeprouter.top'
irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex
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

终端里不带参数运行 `deeprouter` 会进入向导。提示 `Programs [1]:`，直接回车只配置 Codex / ChatGPT。Claude Code 和 Gemini CLI 要自己选。

非交互用 `setup`。`--apps` 只写列出的程序。模型 id 以 `deeprouter models` 的结果为准；下面的 id 只是示例。省略模型标志时，setup 会从网关的对话模型列表里按家族挑一个（Claude 含 `claude`，Codex 含 `gpt`，Gemini 含 `gemini`）。

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

写入 `~/.codex/config.toml`（`CODEX_HOME` 可改目录；Windows 默认 `%USERPROFILE%\.codex\config.toml`）。`base_url` 带 `/v1`，key 在 `experimental_bearer_token`。`wire_api` 默认 `responses`。配完后退出并重新打开 Codex / ChatGPT。

### Gemini CLI

```sh
npm install -g @google/gemini-cli
```

macOS 和 Linux 也可以 `brew install gemini-cli`。

```sh
deeprouter setup --apps gemini --gemini-model gemini-2.5-pro
```

`GEMINI_API_KEY` 和 `GOOGLE_GEMINI_BASE_URL` 写入 shell 启动文件（Windows 是用户环境变量）。基址是网关源站，例如 `https://deeprouter.top`，不带 `/v1`。`~/.gemini/settings.json` 里会设置 `security.auth.selectedType = gemini-api-key`，选了模型时写入 `model.name`。新开一个终端后再运行 `gemini`。

## 测试连通性

```sh
deeprouter test
```

检查当前 profile 里配置过的程序：Codex / ChatGPT 是 `GET /v1/models` 和 `POST /v1/chat/completions`，Claude Code 是 `POST /v1/messages`，Gemini CLI 是 `POST /v1beta/models/{model}:generateContent`。打印 HTTP 状态、首字节时间和总时间。`--json` 输出 JSON。没选模型时，从网关对话模型列表里选同家族 id；列表里没有就跳过该检查，不算失败。

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

当前 Codex 已删除 `disable_response_storage`，看到这行就忽略它。deeprouter 不写这个键。打开 `~/.codex/config.toml`（Windows：`%USERPROFILE%\.codex\config.toml`），删掉根表里的 `disable_response_storage = ...`。如果 `[model_providers.deeprouter]` 里也有这一行，一并删掉。其他表里的同名键不用动。

### npm 很慢

向导发现 npmjs.org 短时间连不上，或语言 / 时区像中国大陆时，会先提到你可以自己执行：

```sh
npm config set registry https://registry.npmmirror.com
```

然后问 `Use https://registry.npmmirror.com for this install only? [y/N]`，再问 `Install missing CLIs with npm? [y/N]`。两个都答 `y` 才会执行 `npm install -g --registry https://registry.npmmirror.com ...`。程序不会替你改已保存的 npm 配置。`--install-clis` 不会出现这个提问。

### Codex 返回 400

`deeprouter test` 在 4xx 时会打印 `HTTP 400.`，后面是网关错误信息（key 已打码）。Codex 自己走 Responses API（`wire_api = "responses"`）。测试只打 `GET /v1/models` 和 `POST /v1/chat/completions`，测试通过并不表示 Codex 发出的 Responses 请求也会成功。

先看那句错误。模型不对时运行 `deeprouter models`，再用网关列出的 gpt 对话模型重配：

```sh
deeprouter setup --apps codex --codex-model gpt-6.1-sol
```

当前 Codex 会拒绝 `wire_api = "chat"`，那是配置解析错误，不是这个 HTTP 400。不要把 `wire_api` 改成 `chat`。

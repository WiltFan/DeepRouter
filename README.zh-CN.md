[English](README.md) | **简体中文**

# deeprouter

一条命令，把兼容 OpenAI 与 Anthropic 的 API 网关接到你选定的编程工具上。

![终端演示：安装 v0.3.2，配置 Codex / ChatGPT，连通性测试通过](docs/demo.gif)

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

`install.sh` 是 POSIX `sh`，只覆盖 macOS 和 Linux。`install.ps1` 是 Windows 安装程序。在仓库旁边时，两者都用 Go 1.22 或更新版本编译一个静态二进制。二进制里的版本号是 `git describe --tags`；没有标签时是 `dev`。从网上安装时，它们下载普通的发布二进制（不是压缩包），并用同一位置的 `checksums.txt` 校验。默认地址是 `https://github.com/zfg5118/deeprouter/releases/latest/download/`。这一步失败、且仓库是 `zfg5118/deeprouter` 时，会再试 `https://deeprouter.top/releases/latest/`。`DEEPROUTER_DOWNLOAD_BASE` 指定先试哪个地址。镜像地址的路径是 `releases/<版本或 latest>/<文件>`，不是 GitHub 的 `releases/download/`。这一步不调用 GitHub API。文件名是 `deeprouter-darwin-amd64`、`deeprouter-darwin-arm64`、`deeprouter-linux-amd64`、`deeprouter-linux-arm64`、`deeprouter-windows-amd64.exe`、`deeprouter-windows-arm64.exe`。`DEEPROUTER_SETUP_VERSION` 不带前导 `v`（例如 `0.1.1`）在 GitHub 上对应 `releases/download/v0.1.1/`，在镜像上对应 `releases/v0.1.1/`。Fork 之后设置 `DEEPROUTER_SETUP_REPO=owner/name`。Fork 不会自动改用 `deeprouter.top`，除非设置了 `DEEPROUTER_DOWNLOAD_BASE`。安装完成后没有运行时依赖。

## 中国大陆用户

`raw.githubusercontent.com` 和 GitHub Releases 在中国大陆经常很慢或打不开。网关 `https://deeprouter.top` 本身可以访问，也托管了安装脚本和发布文件。

macOS 和 Linux：

```sh
curl -fsSL https://deeprouter.top/install.sh | sh
```

Windows PowerShell：

```powershell
irm https://deeprouter.top/install.ps1 | iex
```

cmd：

```bat
powershell -ExecutionPolicy Bypass -c "irm https://deeprouter.top/install.ps1 | iex"
```

GitHub 上的安装脚本先访问 GitHub，失败后再试 `https://deeprouter.top`。镜像上的那一份脚本先访问镜像，失败后再回到 GitHub。`install.sh` 和 `install.ps1` 都给第一个地址的每个文件 45 秒，给第二个地址的每个文件 120 秒。两种都会打印实际用的是哪一个地址，并且仍然校验 `checksums.txt`。校验和对不上，或者下载到的是网页而不是文件，都算这个地址失败，接着试下一个。`install.ps1` 会先在当前会话里打开 TLS 1.2，因为旧版 Windows PowerShell 默认不提供。想强制先用镜像时：

```sh
DEEPROUTER_DOWNLOAD_BASE=https://deeprouter.top curl -fsSL https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.sh | sh
```

PowerShell：

```powershell
$env:DEEPROUTER_DOWNLOAD_BASE = 'https://deeprouter.top'
irm https://raw.githubusercontent.com/zfg5118/deeprouter/main/install.ps1 | iex
```

镜像上的 `install.sh` / `install.ps1` 已经把默认地址写成 `https://deeprouter.top`，一般不用再设这个变量。版本号在环境变量里不带前导 `v`（`DEEPROUTER_SETUP_VERSION=0.3.3`），URL 里带 `v`（`/releases/v0.3.3/`）。

### 服务器要提供的路径

在能访问 GitHub 的机器上生成目录（不会打 tag，也不会上传）：

```sh
sh scripts/mirror-layout.sh --version v0.3.3
```

把 `dist-mirror/` 里面的内容上传到站点根目录，使 `dist-mirror/install.sh` 对应 `https://deeprouter.top/install.sh`。`latest/` 是文件副本，不要做成符号链接。`latest.txt` 只有一行 `v0.3.3` 加换行，给运维核对当前版本；安装程序不读这个文件。

| URL | 内容 |
| --- | --- |
| `https://deeprouter.top/install.sh` | 安装脚本，默认先用镜像 |
| `https://deeprouter.top/install.ps1` | Windows 安装脚本，默认先用镜像 |
| `https://deeprouter.top/releases/latest.txt` | `v0.3.3` 和换行 |
| `https://deeprouter.top/releases/latest/<文件>` | 与下面版本目录相同的字节 |
| `https://deeprouter.top/releases/v0.3.3/<文件>` | `checksums.txt` 和 6 个二进制 |

`<文件>` 是 `checksums.txt`、`deeprouter-darwin-amd64`、`deeprouter-darwin-arm64`、`deeprouter-linux-amd64`、`deeprouter-linux-arm64`、`deeprouter-windows-amd64.exe`、`deeprouter-windows-arm64.exe`。GitHub 上的对应地址仍是 `https://github.com/zfg5118/deeprouter/releases/latest/download/<文件>` 和 `https://github.com/zfg5118/deeprouter/releases/download/v0.3.3/<文件>`。

换版本时重新跑 `scripts/mirror-layout.sh --version vX.Y.Z`，再上传。`--assets 目录` 可以改用已经下好的文件，不必当场访问 GitHub。

### npm 镜像

向导问「用 npm 安装缺少的命令行工具？」并得到肯定回答之后，如果 npmjs.org 连不上，或者语言 / 时区像中国大陆（例如 `TZ=Asia/Shanghai`、`LANG=zh_CN`），会说明可以自己执行：

```sh
npm config set registry https://registry.npmmirror.com
```

接着问要不要只给这一次安装加上 `--registry https://registry.npmmirror.com`（`这一次安装使用 https://registry.npmmirror.com ？`）。两个问题都回答是，才会执行 `npm install -g --registry https://registry.npmmirror.com ...`。镜像问题回答 `n` 则用默认的 npm 源。安装问题回答 `n` 则什么都不装，也不会再问镜像。程序不会擅自执行 `npm config set`，也不会改你的全局 npm 配置。

## 快速开始

```sh
deeprouter
```

在终端里不带参数运行，会进入交互式配置。`install.sh` 和 `install.ps1` 在有控制终端时会在装完后直接进入这一步。程序只改你勾选的那些工具，其余文件不动。

界面默认是简体中文。只有语言环境明确是英文时才用英文：`LC_ALL`（为空时看 `LANG`）是 `en`、`en_US.UTF-8`、`en-GB` 这类值。`C`、`POSIX` 和空值仍是中文。`--lang en` 或 `DEEPROUTER_LANG=en` 选英文，`--lang zh` 或 `DEEPROUTER_LANG=zh` 选中文，不管语言环境是什么。`DEEPROUTER_LANG` 是别的值时忽略它。这条规则对所有命令都一样，包括 `setup`、`test` 和它们的报错，两个安装脚本也一样。

标准输入不是终端时（`deeprouter </dev/null`，或者管道），向导不会把输入当作回答。它打印 `请在终端中运行 deeprouter。`（英文是 `Run deeprouter in a terminal.`）和 `deeprouter setup` 的用法，不发请求，不写文件，退出码是 2。

1. 先选择平台。提示是 `平台 [1]:`（英文是 `Platform [1]:`）。`1` 是 DeepRouter（`https://deeprouter.top`，默认），`2` 是 WolfAI（`https://wolfai.top`），`3` 是 LinkMesh（`https://www.linkmesh.org`），`4` 填写自定义网关地址。直接回车用当前高亮的默认项。`DEEPROUTER_PROVIDER=wolfai`（或 `linkmesh`）会把默认项改成那一家。
2. 再选择要配置的程序，可以多选。提示里有 `多选示例：1,2（英文逗号、中文逗号、顿号或空格都可以分隔）` 和 `程序 [1]:`（英文是 `Example: 1,2` 和 `Programs [1]:`）。直接回车只选 Codex / ChatGPT。分隔符可以是逗号、空格、全角逗号 `，`、顿号 `、` 或全角空格。Codex CLI 和 ChatGPT 桌面应用共用 `~/.codex/config.toml`，所以列表里只有一项。选出来的结果里一个可配置程序都没有时，会重新问。选中的程序如果还没安装，会马上说明配置照常写入，安装之后即可使用，并给出官方安装命令。这不是失败，结束时的 `接下来：` 里会再打印一次。查找时除了当前 `PATH`，还会看常见的 npm 全局目录和 Homebrew 目录（`/opt/homebrew/bin`、`/usr/local/bin`、`~/.npm-global/bin` 等）。Claude Code 是 `curl -fsSL https://claude.ai/install.sh | bash`（Windows 是 `irm https://claude.ai/install.ps1 | iex`），Codex / ChatGPT 是 `npm install -g @openai/codex`，Grok Build 是 `curl -fsSL https://x.ai/cli/install.sh | bash`（Windows 是 `irm https://x.ai/cli/install.ps1 | iex`），Claude Desktop 是 https://claude.com/download ，Gemini CLI 是 `npm install -g @google/gemini-cli`，macOS 和 Linux 另外还有 `brew install gemini-cli`。
3. 只有自定义平台才问网关地址。提示是 `网关地址（直接回车使用 https://deeprouter.top，或填写其他兼容接口）：`。任何兼容的接口地址都可以。格式不对会要求重填。从这个地址拉不到模型列表时，会再问一次地址（`网关地址（直接回车重试 …）：`）。选项 `1`–`3` 跳过这一问，直接用该平台的地址。
4. **对每一个选中的程序单独**询问令牌和模型。提示会写明平台，例如 `请输入 WolfAI 的令牌（在 wolfai.top 创建），用于 Codex / ChatGPT：`（英文是 `Enter the WolfAI token (create one at wolfai.top) for Codex / ChatGPT: `）。自定义网关则写明地址：`请输入该网关的令牌（https://gateway.example.com），用于 Codex / ChatGPT：`。别的平台保存的令牌不会被拿来用。同一平台已有令牌时，会在方括号里显示，例如 `……，用于 Codex / ChatGPT [sk-n…Mc04]：`，直接回车保留，并打印 `已保留（sk-****abcd，51 个字符）`。输入时每个字符回显一个 `*`。提交后打印 `已输入（sk-****abcd，51 个字符）`。后面的程序如果还没有自己的令牌，提示是 `Claude Code 令牌（直接回车沿用 sk-n…Mc04）：`，回车沿用上一个程序刚确定的令牌，并打印 `已沿用（sk-****abcd，51 个字符）`。什么都不输入就回车时，打印 `令牌不能为空，请重新输入。`，再问一次。

   令牌先当场检查：向导拉取模型列表，并发送很小的测试请求。网关返回 401 或 403 时，会打印网关给的原因，例如 `网关拒绝了这个令牌（401）。网关返回：Invalid token`，或 `网关拒绝了这个令牌（403）：额度不足或没有权限。`，然后要求重新输入令牌。测试请求按网关推荐的顺序逐个试这个家族的对话模型，直到有一个应答。没有应答的模型会说明原因，例如 `已跳过 gpt-5.5：503 没有可用渠道。网关返回：……`，并且不出现在列表里。模型问题是这样的：

   ```text
   Codex / ChatGPT 模型（直接回车不指定模型，使用客户端默认）：
     1) 不指定模型（使用客户端默认）
     2) gpt-5.4（已通过测试）
     3) gpt-4.1
   Codex / ChatGPT 模型 [1]:
   ```

   第 1 项、直接回车或输入 `default` 都不写模型，并删掉以前写过的模型行，让程序用自己的默认模型。有模型通过测试时，第 2 项就是它。其它序号是该家族其余的对话模型 id（Codex 列出 id 里含 `gpt` 的，Claude 含 `claude`，Gemini 含 `gemini`，Grok 含 `grok`）。也可以直接输入列表里的 id。还没测试过的模型，选中后先发一次测试请求，没通过就再问。其它输入，例如 `n`、`yes` 或超出列表的序号，会打印 `这不是模型 id。请输入列表中的编号或 id，或直接回车。`（或 `模型 99 超出范围。……`），再问一次。网关没有列出这个家族的模型时，回车不指定模型，也可以输入一个 id。图片、视频、音频、向量、语音和审核模型不会出现，包括 id 里含 image、nano-banana、dall-e、sora、veo、imagen、embedding、tts、whisper、audio、realtime、moderation、flare、sunburst 的项。网关如果在模型对象上给了类型或能力字段，以那个字段为准。某个家族被滤空时，退回这个家族滤前的列表。同一个令牌只拉一次模型列表，每个家族也只测试一次。
5. 写入之前会列出将要修改的文件，并询问 `继续写入？[Y/n]`（英文是 `Continue? [Y/n]`）。直接回车确认。回答 `n` 会打印 `已取消配置，没有写入任何文件。`，退出码是 2。终端向导不改 shell 启动文件，但 Gemini CLI 例外，因为它从环境变量读 key 和基址。macOS 上的图形界面本来也不会读 `~/.zshrc`。
6. 确认之后写入文件，并对选中的程序做连通性测试。没有指定模型的程序，用一个能应答的模型发测试请求，显示为 `gpt-4.1（仅用于测试，未写入配置）`，这个 id 不会写入。测试没通过时，向导说明文件已经写入，并给出 `1) 换一个模型` 和 `2) 撤销刚才的写入`（`选择 [2]:`）。选 `1` 会重新询问测试失败的那些程序的模型，写入后再测一次。直接回车、`2` 或 Ctrl-D 会把文件恢复成这次运行之前的样子，退出码是 1。
7. 选中的 npm CLI 如果还没安装，接着会问 `用 npm 安装缺少的命令行工具？`（英文是 `Install missing CLIs with npm?`）。环境不像中国大陆时默认是否（`[y/N]`），只有回答 `y` 才安装。回答是之后，如果 npmjs.org 可能较慢（语言或时区像中国大陆，或者 npmjs.org 连不上），会提到 `npm config set registry https://registry.npmmirror.com`，并问 `这一次安装使用 https://registry.npmmirror.com ？`。语言或时区像中国大陆时，这两个问题都默认是（`[Y/n]`），并说明这不会改已保存的 npm 配置。程序不会执行 `npm config set`。

写入之前，在任何问题上按 Ctrl-D 都会取消，打印 `已取消配置，没有写入任何文件。`，退出码是 2。写入之前按 Ctrl-C 打印同一句话，退出码是 130。写入之后、连通性测试通过之前按 Ctrl-C，会撤销刚才的写入（`已中断，已撤销刚才的写入。`），退出码是 130。测试通过之后再按 Ctrl-C（例如在 npm 问题上），文件保持写入后的样子，并说明这一点（`已中断。上面写入的文件保持不变。`）。

脚本里用 `setup` 和标志：

```sh
export DEEPROUTER_API_KEY=sk-your-key
deeprouter setup
deeprouter setup --provider wolfai
deeprouter setup --provider linkmesh --base-url https://example.com
deeprouter setup --apps codex,claude-code
deeprouter setup --apps grok --grok-model grok-4 --grok-key "$GROK_KEY"
deeprouter test
```

每个平台的令牌是分开的。在终端里运行 `setup` 而没有令牌时，会问一次并写明站点，例如 `请输入 WolfAI 的令牌（在 wolfai.top 创建）：`，英文是 `Enter the WolfAI token (create one at wolfai.top): `。别的平台保存的 key 不会拿来复用。`DEEPROUTER_API_KEY` 只代表 DeepRouter 的令牌。LinkMesh 是独立网关：连通性检查只调用 `/v1/models` 和对话接口，不调用 new-api 的 `/api/status`。

LinkMesh（`https://www.linkmesh.org`）跑的是 sub2api，不是 new-api。不带 key 时，`POST /v1/chat/completions`、`POST /v1/responses`、`POST /v1/messages` 返回 401 `API_KEY_REQUIRED`，`POST /v1beta/models/{model}:generateContent` 返回 Gemini 格式的 401。`POST /v1/models/{model}:generateContent` 是 404，所以 Gemini 的基址是站点根，由 CLI 自己访问 `/v1beta`。Codex 写入 `wire_api = "responses"`，`base_url` 为 `https://www.linkmesh.org/v1`。Claude Code 和 Claude Desktop 使用不带 `/v1` 的 `https://www.linkmesh.org`。Grok Build 走 chat completions。这五个程序都会配置。某个程序对应的路由不存在时会跳过，并提示，例如 `LinkMesh does not expose POST /v1/messages, so Claude Code is skipped.`（中文向导是 `LinkMesh 不提供 POST /v1/messages，已跳过 Claude Code。`）。

不带 `--apps` 的 `setup` 只写 Codex / ChatGPT，和向导的默认一样。`--apps` 只写列出的程序，`--claude-only` 写 Claude Code 和 Claude Desktop。`--claude-only` 和 `--codex-only` 不能和 `--apps` 一起用。`setup` 不改 shell 启动文件，Gemini CLI 除外。没传某个程序的模型标志时不写模型，和传 `default` 一样。

`setup` 在写入任何文件之前先检查令牌：打印 `正在确认 <网关> 的令牌……`（英文是 `Checking the token at <gateway> ...`），然后拉取模型列表，并给每个程序发一次很小的请求。网关返回 401 或 403，或者 `--*-model` 指定的模型没有应答时，打印网关给的原因和 `没有写入配置。`，退出码是 1。没有指定模型的程序用一个能应答的模型测试，这个 id 不会写入；一个都没有应答时，会说明情况，配置仍会写入。完全连不上网关时，会说明没能确认令牌，配置仍会写入，并提示网关可以访问后运行 `deeprouter test`。

## 命令

| 命令 | 作用 |
| --- | --- |
| `setup` | 为一个 profile 写入所选程序的配置 |
| `test` | 检查当前 profile 里保存的那些程序：先 `GET /v1/models`，再给每个程序发一次单 token 请求。Codex / ChatGPT 用 `POST /v1/responses`（`wire_api = "chat"` 时用 `POST /v1/chat/completions`），Claude Code 和 Claude Desktop 用 `POST /v1/messages`，Grok Build 用 `POST /v1/chat/completions`，Gemini CLI 用 `POST /v1beta/models/{model}:generateContent`（请求头 `x-goog-api-key`）。没有指定模型的程序，用该家族里能应答的对话模型测试，并标明 `（仅用于测试，未写入配置）`。这时不会用图片、视频、音频、向量、语音或审核 id；这类 id 被滤空才退回原列表。打印 HTTP 状态、首字节时间和总时间 |
| `models` | 列出网关上的模型 id |
| `switch` | 切换 profile，或改某一个程序的模型 |
| `profile list\|add\|use\|remove` | 管理具名 profile。一个 profile 保存一把 API key |
| `status` | 当前 profile、每个工具是否已接上、`claude` 和 `codex` 是否已安装（含版本） |
| `restore` | 从备份还原被改过的文件 |
| `uninstall` | 还原这些文件。`--purge` 同时删除 `~/.deeprouter` |
| `version` | 打印版本 |

`deeprouter <command> --help` 可以看标志。`test` 和 `models` 接受 `--json`。所有命令和向导都接受 `--lang zh` 或 `--lang en`。

退出码：`0` 成功。`1` 网关检查没通过、令牌被拒绝、写文件失败，或者向导撤销了写入。`2` 命令行不正确、写入之前取消了配置（回答 `n` 或按 Ctrl-D），或者不在终端里启动向导。`130` 按了 Ctrl-C。

## 非交互标志

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

| 标志 | 作用 |
| --- | --- |
| `--provider NAME` | `deeprouter`（默认，`https://deeprouter.top`）、`wolfai`（`https://wolfai.top`）或 `linkmesh`（`https://www.linkmesh.org`）。`DEEPROUTER_PROVIDER` 选的是同一组平台。`--base-url` 只覆盖地址，平台 id 不变。Codex 写入 `model_provider = "<id>"` 和 `[model_providers.<id>]`。Grok Build 写入 `[model.<id>]`。再次配置会删掉上一次的托管段（`deeprouter`、`wolfai`、`linkmesh` 或 `custom`），其它表不动。`install.sh` 和 `install.ps1` 会把 `--provider` 和 `DEEPROUTER_PROVIDER` 传给后面的配置 |
| `--apps LIST` | `claude-code`、`claude-desktop`、`codex`、`grok`、`gemini`，逗号或空格分隔 |
| `--api-key` / `--api-key-stdin` / `DEEPROUTER_API_KEY` | 各程序共用的 key。某个程序没单独给 key 时用它 |
| `--claude-code-key`、`--claude-desktop-key`、`--codex-key`、`--grok-key`、`--gemini-key` | 只写入对应程序。省略时用共用 key |
| `--claude-model`、`--claude-desktop-model`、`--codex-model`、`--grok-model`、`--gemini-model` | 对应程序的模型 id。写入之前先发一次很小的测试请求，没有应答时 `setup` 以退出码 1 结束，不写任何文件。省略这个标志或传 `default` 时不写模型，并删掉以前写过的模型行，让程序用自己的默认模型：Codex 删掉根表的 `model`，Claude Code 删掉 `ANTHROPIC_MODEL`，Grok Build 删掉托管表里的 `model`，Gemini CLI 删掉 `model.name`。Claude Desktop 删掉只有一项的 `inferenceModels`，并设置 `modelDiscoveryEnabled`；列了不止一个模型时保持不动。没传 `--claude-desktop-model` 时，`--claude-model` 也用于 Claude Desktop |
| `--no-shell` | 为旧脚本保留。`setup` 只在配置 Gemini CLI 时改 shell 启动文件，`--no-shell` 也不会跳过，因为 Gemini CLI 从环境变量读 `GEMINI_API_KEY` 和 `GOOGLE_GEMINI_BASE_URL` |
| `--claude-only` | 写 Claude Code 和 Claude Desktop，不写 Codex / ChatGPT。不能和 `--apps` 一起用 |
| `--codex-only` | 只写 Codex / ChatGPT，这也是不带 `--apps` 时的默认 |

`--api-key` 会出现在进程列表里。脚本里更适合用 `DEEPROUTER_API_KEY` 或 `--api-key-stdin`。

`switch` 只更新上次实际写入过的那些程序。一个只配过 Grok Build 的 profile，切换时不会改 Claude Code 或 Codex / ChatGPT。模型可以用 `--claude-model`、`--claude-desktop-model`、`--codex-model`、`--grok-model`、`--gemini-model` 单独改。

## Profile

一个 profile 保存网关地址、API key 和可选的模型 id。名字自己定，不再有 official-key 分组。

```sh
deeprouter setup --api-key "$DEEPROUTER_API_KEY"
deeprouter profile add azure --label Azure --api-key "$AZURE_KEY"
deeprouter switch --profile azure
```

切换 profile 会重写上次配置过的那些文件。重复运行会就地替换，不会在 `~/.zshrc` 里追加第二段，也不会把同一个 key 写两遍。

## 每个程序写到哪里

下面的模型 id 以你确认过的网关列表为准。只有在向导里选了模型，或传了 `--*-model` 标志时才写模型这一行；默认不写。带 `/v1` 或不带都可以作为 `--base-url`：Claude 拿到的地址没有末尾 `/v1`（客户端自己请求 `/v1/messages`），Codex / ChatGPT 和 Grok Build 拿到的地址带 `/v1`。

### Claude Code

`~/.claude/settings.json`（Windows 是 `%USERPROFILE%\.claude\settings.json`）。其它键会保留。文件模式 `0600`。这里的环境变量优先于 shell。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://deeprouter.top",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-key",
    "ANTHROPIC_MODEL": "claude-sonnet-5-5"
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
  "inferenceModels": [{ "name": "claude-sonnet-5-5" }]
}
```

`inferenceGatewayBaseUrl` 没有末尾 `/v1`。应用内示例就是这个形状（`https://llm-gateway.example.corp`）。Code 自己请求 `POST /v1/messages`。`GET /v1/models` 是可选的。只有一个模型 id 时写入 `inferenceModels`，除非文件里已经列了不止一个模型。没有选模型时，删掉只有一项的 `inferenceModels`，并把 `modelDiscoveryEnabled` 设为 true，让应用自己列出网关的模型；列了不止一个模型时保持不动。发现功能只展示应用认得出来的 Claude 模型 id。

两个文件都是模式 `0600`。托管配置会盖过这个目录：macOS 配置描述文件、Windows 机器策略，或必须由 root 拥有的 Linux 文件 `/etc/claude-desktop/managed-settings.json`。本工具不写这些位置。一个错误的 `/etc` 文件会让本地设置失效，而且它必须属于 root。

设置 `inferenceProvider` 会让应用进入第三方模式。除非登录时已经选了 Claude.ai，应用会以这个模式打开。本工具不设置 `disableDeploymentModeChooser`，所以 Claude.ai 登录仍然可选。如果之前选过 Claude.ai，需要在打开 Developer Mode 之后走 Developer → Configure Third-Party Inference，或者跳过 Anthropic 登录，然后退出并重新打开应用。

### Codex / ChatGPT

Codex CLI 和 ChatGPT 桌面应用共用 `~/.codex/config.toml`。`CODEX_HOME` 可以改这个目录。Windows 默认是 `%USERPROFILE%\.codex\config.toml`。应用里：Settings → Configuration → Open config.toml。列表里只有 Codex / ChatGPT 一项，写一次就同时生效。IDE 扩展也读这个文件。

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

`model` 和 `model_provider` 留在第一个 `[table]` 之前的根表里，Codex 从那里读。注释和无关的表会保留。文件模式 `0600`。

id 跟着平台走。`--provider wolfai` 写入 `model_provider = "wolfai"` 和 `[model_providers.wolfai]`，`base_url` 是 `https://wolfai.top/v1`。LinkMesh 用 `linkmesh` 和 `https://www.linkmesh.org/v1`。向导里的自定义地址用 `custom`。下次配置会删掉上一个托管段，不会动 `[model_providers.openai]` 或其它表。

Codex 已移除 `disable_response_storage`（请求不再被存储）。本工具不写这个键。下次配置 Codex / ChatGPT 时，会从根表和当前托管的 provider 段里删掉它。其他表里的同名键保持不动。

key 放在 `experimental_bearer_token` 上，应用服务器和从图形界面启动的进程直接读 `config.toml`，不需要环境变量。这里不写 `env_key`：一旦写了，Codex 会在看 bearer token 之前返回 `Missing environment variable: DEEPROUTER_API_KEY`。

`~/.codex/auth.json`（或系统钥匙串，当 `cli_auth_credentials_store` 是 `keyring` 或 `auto`）是 ChatGPT / OpenAI API key 的登录缓存，不是自定义 provider 的凭据。本工具不把网关 key 写进去。

当前 Codex / ChatGPT 走 Responses API，所以 `wire_api` 默认是 `responses`。当前 Codex 只接受 `responses`，会拒绝 `wire_api = "chat"`。旧版本仍可以传 `--codex-wire-api chat`。向导里的连通性测试只调用这次选中的程序对应的端点。Codex / ChatGPT 检查 `GET /v1/models` 和 `POST /v1/responses`（`wire_api = "chat"` 时是 `POST /v1/chat/completions`），和 Codex 自己用的接口一样。Claude Code 和 Claude Desktop 检查 `POST /v1/messages`。Grok Build 检查 `POST /v1/chat/completions`。Gemini CLI 用 `x-goog-api-key` 检查 `POST /v1beta/models/{model}:generateContent`。没有指定模型时，测试用同家族里能应答的对话模型，并且不保存它。网关列表里没有对应家族的模型时跳过该检查，不算失败。`deeprouter test` 检查当前配置里保存的那些程序。

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

凭据顺序是 `api_key` > `env_key` > 已登录会话 > `XAI_API_KEY`。文档更建议用 `env_key`，避免把 key 写进文件。本工具仍然把 `api_key` 写进这个文件，并删掉托管的 `[model.<id>]` 里的 `env_key`，这样从 Dock 或没有 shell 环境的方式启动时也能读到 key。文件模式 `0600`。段落 id 就是平台 id（默认 `deeprouter`，除非 `--provider` 或 `DEEPROUTER_PROVIDER` 指定了别的）。换模型只改 `model` 字段。其它 `[model.*]`、`[mcp_servers.*]`、注释，以及 `[models].web_search` 会保留。`--provider wolfai` 写成 `default = "wolfai"` 和 `[model.wolfai]`，并删掉上一个托管段。

项目里的 `.grok/config.toml` 只贡献 MCP、插件和权限规则，完整的用户配置只从 `~/.grok/config.toml` 读。本工具不写项目文件，也不写 `managed_config.toml`、`requirements.toml` 或 MDM。那些层的优先级更高，会盖过用户文件。

`GROK_MODELS_BASE_URL` 配合 `XAI_API_KEY` 也可以做环境变量方式的模型发现。图形界面或非 shell 启动看不到 shell 环境，所以不用它作为写入方式。改完配置后新开一个 Grok Build 会话，它才会重新读 `config.toml`。

### Gemini CLI

Gemini CLI 走 Gemini 的 `generateContent` 接口。`@google/genai` 把 `GOOGLE_GEMINI_BASE_URL` 当作基址，再接上 API 版本和路径，默认请求是 `{基址}/v1beta/models/{model}:generateContent`。所以这里写入的基址就是网关源站，例如 `https://deeprouter.top`，不带 `/v1`，也不带 `/v1beta`。鉴权用 `GEMINI_API_KEY`（请求头 `x-goog-api-key`）。Gemini CLI 要求这个基址是 HTTPS，只有 `localhost`、`127.0.0.1` 和 `[::1]` 可以用 HTTP。

这两个变量写进和其它环境变量同一处：macOS 和 Linux 是 shell 启动文件里的标记段，Windows 是用户环境 `HKCU\Environment`。`restore` 和 `uninstall` 会把它们恢复成第一次写入之前的值。`--apps gemini` 也会写这两个变量。`--no-shell` 不会跳过它们。

用户设置在 `~/.gemini/settings.json`（`GEMINI_CLI_HOME` 可以改 `.gemini` 的父目录）。Windows 默认是 `%USERPROFILE%\.gemini\settings.json`。本工具只合并两处，不覆盖文件里的其它字段：

- `security.auth.selectedType` 设为 `gemini-api-key`，这样启动时不再询问 Google 登录。
- 选了模型时写入 `model.name`。交互列表第 1 项、回车、省略 `--gemini-model`，或 `--gemini-model default` 会删掉 `model.name`，让 Gemini CLI 用自己的默认模型。`model` 里的其它字段保留。

还没安装时，向导和 `setup` 会提示官方命令 `npm install -g @google/gemini-cli`，文档在 https://geminicli.com/docs/get-started/installation/ 。配置仍会写上。项目里的 `.gemini/settings.json` 会盖过用户设置。系统设置在 Linux 的 `/etc/gemini-cli/settings.json`、Windows 的 `C:\ProgramData\gemini-cli\settings.json`、macOS 的 `/Library/Application Support/GeminiCli/settings.json`。文档：[配置](https://geminicli.com/docs/reference/configuration/)。

### Shell 启动文件

在 macOS 和 Linux 上，只有配置 Gemini CLI 时才会在 `~/.zshrc`、`~/.bashrc` 或 `~/.config/fish/config.fish` 里写一段带标记的导出，因为 Gemini CLI 从环境变量读 `GEMINI_API_KEY` 和 `GOOGLE_GEMINI_BASE_URL`。已经打开的终端可以 `source` 它。向导和 `setup` 不给 Claude Code 或 Codex / ChatGPT 写这段，它们读自己的配置文件。旧版本写进这一段的变量会保留，直到 `restore`。Windows 上不改 `.zshrc` 或 PowerShell profile。同一组变量写到当前用户的环境变量（注册表 `HKCU\Environment`，效果和 `setx` 一样，但没有 1024 字符截断）。新开的进程才会继承它们。`restore` 和 `uninstall` 会把这些变量恢复成第一次写入之前的值。传 `--rc-file` 时，Windows 也改为改那个启动文件，不写用户环境变量。Claude Code、Codex / ChatGPT、Claude Desktop 和 Grok Build 不依赖这段导出。

```sh
# >>> deeprouter-setup >>>
export GOOGLE_GEMINI_BASE_URL="https://deeprouter.top"
export GEMINI_API_KEY="sk-your-key"
# <<< deeprouter-setup <<<
```

再次运行会替换这一段，不会再追加一份。

## 接不上这个网关的情况

登录 Claude.ai 的普通聊天不走自定义网关。Slack 里的 Claude Code，以及 Claude Code 云会话，都不属于网关部署：云会话里的网关变量不会生效。桌面网关配置生效时，Remote Control 不可用。这些界面继续使用 Anthropic。

托管策略优先于本地文件：Claude Desktop 的 MDM / `/etc/claude-desktop/managed-settings.json`，以及 Grok Build 的 `managed_config.toml`、`requirements.toml` 和 MDM。本工具不写这些位置。

## 常见问题

### API key 放在哪？

key 只写进你配置的那些程序的文件，以及 profile：

- `~/.deeprouter/config.json`，模式 `0600`，每次都写。每个程序的 key 不一样时，profile 里会多存一份覆盖值
- Codex / ChatGPT：`~/.codex/config.toml` 里 `[model_providers.<id>]`（DeepRouter 是 `[model_providers.deeprouter]`）的 `experimental_bearer_token`。Codex / ChatGPT 的 CLI、桌面应用和 IDE 扩展读它。写完后会核对这个值和 profile 一致，并且 provider 没有 `env_key`。`auth.json` 不动
- Claude Code：`~/.claude/settings.json` 的 `env.ANTHROPIC_AUTH_TOKEN`。Claude Code CLI 读它。Claude Desktop 的 Code 标签不拿它当网关凭据
- Claude Desktop：`configLibrary/` 里当前启用的文件，字段是 `inferenceGatewayApiKey`
- Grok Build：`~/.grok/config.toml` 里托管的 `[model.<id>]` 表的 `api_key`
- Gemini CLI：shell 启动文件里的 `GEMINI_API_KEY`。Windows 上它在用户环境变量里，不在启动文件里

不带 `--apps` 的 `setup`，以及在向导的程序问题上直接回车，都只配置 Codex / ChatGPT。`GET /v1/models`、Responses 请求和 chat completion 用 `Authorization: Bearer`。messages 检查还会带 `x-api-key` 和 `anthropic-version`，Gemini 检查带 `x-goog-api-key`。不跟随重定向。

`deeprouter status` 会打印 `claude` 和 `codex` 是否在 `PATH` 上，以及各自 `--version` 的第一行。向导结束后，已安装或未安装的提示，以及 `用 npm 安装缺少的命令行工具？[y/N]`（中国大陆环境是 `[Y/n]`；英文界面是 `Install missing CLIs with npm?`），只覆盖这次选中的程序。没安装的程序会说明配置已经写好，装上之后就能用。Codex / ChatGPT 会说明桌面应用共用同一份配置，只用桌面应用时回答 N 即可。Claude Desktop 没有 npm 包。Grok Build 打印自己的安装命令，不进入 npm 提问。`setup --install-clis` 对正在配置的程序里缺失的 npm CLI 执行 `npm install -g`（`@anthropic-ai/claude-code`、`@openai/codex`、`@google/gemini-cli`）。没有 `npm` 时改为打印官方安装页，不安装。

同一平台已有 key 时，向导的提示会写明站点并显示这个 key，例如 `请输入 WolfAI 的令牌（在 wolfai.top 创建），用于 Codex / ChatGPT [sk-n…Mc04]：`。回车保留，输入则替换。别的平台保存的 key 不会复用。`DEEPROUTER_API_KEY` 只代表 DeepRouter 的令牌。`setup` 只在没有 key 且 stdin 是终端时才提问。其它情况依次用 `--api-key` 或 `--api-key-stdin`、这个平台已经保存的 key，以及配置 DeepRouter 时的 `DEEPROUTER_API_KEY`。LinkMesh 不会通过 `/api/status` 探测。

`~/.deeprouter/backups` 里是每次修改前的文件副本，所以里面可能有曾经写在 `settings.json` 或 shell rc 里的 key。`deeprouter uninstall --purge` 会删掉这个目录。

### 怎么撤回？

`deeprouter restore` 写回本工具第一次改每个文件之前的副本。本工具新建的文件会被删掉。Profile 保留，可以再跑 `setup`。

`deeprouter restore --list` 列出之后的快照。`deeprouter restore --id latest` 只撤销最近一次修改。

`deeprouter uninstall` 做同样的还原。加上 `--purge` 会删除 `~/.deeprouter`，包括保存的 profile 和备份。

### 能换一个网关吗？

可以。任何提供 `GET /v1/models`，以及你要配置的程序所用路径的基址都可以：Codex / ChatGPT 是 `POST /v1/responses`，Claude Code 和 Claude Desktop 是 `POST /v1/messages`，Grok Build 是 `POST /v1/chat/completions`，Gemini CLI 是 `POST /v1beta/models/{model}:generateContent`。向导里选 `4` 并填写地址，之后的令牌提示会写明这个地址。

```sh
deeprouter setup --base-url https://gateway.example.com --api-key "$GATEWAY_KEY"
```

`test` 会报告 HTTP 状态和一句短提示，后面跟着网关自己的说明：`401` key 不对，`403` 额度或权限，`404` 模型或路径，`429` 请求过于频繁，`503` 这个模型没有可用渠道。

### Windows 能用吗？

可以。发布包含 `deeprouter-windows-amd64.exe` 和 `deeprouter-windows-arm64.exe`，校验和在 `checksums.txt` 里。用上面的 PowerShell 或 cmd 命令安装。交互式提问走 Windows 控制台：输入 API key 时每个字符显示一个 `*`（控制台逐字读取，不用 `stty`，也不打开 `/dev/tty`）。在 `irm ... | iex` 下，安装脚本在你的 PowerShell 会话里运行，配置结束后不管怎么回答都回到提示符，不会关闭窗口。用 `-File .\install.ps1` 运行时，退出码就是向导的退出码，取消时不再多打印。

Claude Code 写 `%USERPROFILE%\.claude\settings.json`，Codex / ChatGPT 写 `%USERPROFILE%\.codex\config.toml`（`CODEX_HOME` 可以改目录），Claude Desktop 的第三方配置写 `%APPDATA%\Claude\configLibrary\`，Grok Build 写 `%USERPROFILE%\.grok\config.toml`（`GROK_HOME` 可以改目录）。环境变量写入用户环境 `HKCU\Environment`，不改 shell 启动文件。

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

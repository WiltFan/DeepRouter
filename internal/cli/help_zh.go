package cli

const rootHelpZH = `deeprouter 把 Claude Code 和 Codex / ChatGPT 等程序接到兼容 OpenAI 和
Anthropic 接口的 API 网关。

用法：
  deeprouter                  交互式配置
  deeprouter <命令> [参数]

在终端里不带参数运行时，交互式配置先选平台（网关），再选要配置的程序。
直接回车只配置 Codex / ChatGPT；多选示例 1,2（英文逗号、中文逗号、顿号
或空格都可以分隔）。然后为每个选中的程序询问令牌和模型。
该程序已经保存过令牌时，直接回车保留它；后面的程序直接回车沿用前面输入
的令牌。模型列表里直接回车不写入模型，客户端使用自己的默认模型；通过
测试请求的模型列在后面。官方直连预设不获取模型列表，使用客户端默认模型。
写入之前会列出要修改的文件，并说明可用 deeprouter restore 撤销，然后直接写入。
界面默认是简体中文；只有 LANG 或 LC_ALL 明确是英文（en、en_US、en-GB），
设置了 DEEPROUTER_LANG=en，或传入 --lang en 时才用英文。
标准输入不是终端时，deeprouter 只提示如何在终端中运行，不会把管道内容
当作回答。非交互配置请用：deeprouter setup。
图片、视频、音频、向量和审核模型不会出现在模型列表里。Gemini CLI 使用
网关的 generateContent 接口。

命令：
  setup       写入程序配置（只写 Codex，除非传入 --apps 或 --claude-only）
  test        检查连通性、HTTP 状态和延迟
  models      列出网关的模型
  switch      切换当前配置或模型
  profile     管理命名配置
  status      显示当前配置和命令行工具版本
  restore     从备份恢复配置文件
  uninstall   恢复配置文件，并可删除本地数据
  version     显示版本
  help        显示这份帮助

默认网关是 https://deeprouter.top（DeepRouter）。其他预设有 WolfAI
（https://wolfai.top）和 LinkMesh（https://www.linkmesh.org）。官方直连
（需海外网络，不经过 DeepRouter）有 OpenAI（https://api.openai.com）、
Anthropic/Claude（https://api.anthropic.com）、Google Gemini
（https://generativelanguage.googleapis.com）和 xAI Grok（https://api.x.ai）。
可传入 --provider deeprouter|wolfai|linkmesh|openai|anthropic|gemini|xai，
或设置 DEEPROUTER_PROVIDER。--base-url 仍然可以覆盖地址。其他兼容
OpenAI 或 Anthropic 接口的中转地址也可以使用。

运行 deeprouter <命令> --help 查看参数。
`

const setupHelpZH = `用法：deeprouter setup [参数]

写入一个网关配置。不带 --apps 时写入该平台的默认程序：Codex / ChatGPT，
但 Anthropic/Claude 默认写 Claude Code，Google Gemini 默认写 Gemini CLI。
--claude-only 写入 Claude Code 和 Claude Desktop。再次运行 setup 会原地
更新同样的文件。令牌被拒绝，或参数指定的模型没有通过测试请求时，直接退出，
不写入任何文件。没有传模型参数的程序不写入模型，客户端使用自己的默认模型；
确认令牌时可能临时用一个能用的模型发请求，这个模型不会写入配置。官方预设
openai、anthropic、gemini、xai 不获取模型列表；这些预设上的模型参数会按原样写入。

参数：
  --provider NAME        deeprouter（默认）、wolfai、linkmesh、openai、
                         anthropic、gemini 或 xai。
                         省略时由 DEEPROUTER_PROVIDER 选择预设。
                         --base-url 仍然可以覆盖地址，并保留所选平台。
                         openai、anthropic、gemini、xai 是官方直连，只提供给
                         能使用该接口的客户端，并且不获取模型列表。
  --base-url URL         网关地址（默认 https://deeprouter.top，或所选平台的地址）。
                         任何兼容 OpenAI 或 Anthropic 接口的中转地址都可以。
  --api-key KEY          令牌。会出现在进程列表里，建议改用环境变量。
  --api-key-stdin        从标准输入读取令牌
  --profile NAME         配置名称（默认：当前配置，或 default）
  --label TEXT           配置的标签
  --apps LIST            claude-code、claude-desktop、codex、grok、gemini。
                         只写入列出的程序。不带 --apps 时写该平台的默认程序。
                         只有 Gemini CLI 的环境变量会写入 shell 启动文件。
  --claude-model ID      Claude Code 的 ANTHROPIC_MODEL。
                         "default" 会删除它，使用客户端默认。
  --claude-desktop-model ID
                         Claude Desktop 的模型。默认跟随 --claude-model。
                         "default" 会删除模型，使用客户端默认。
  --codex-model ID       Codex / ChatGPT 的模型。"default" 会删除它。
  --grok-model ID        Grok Build 使用的模型。"default" 会删除它。
  --gemini-model ID      Gemini CLI 设置里的 model.name。"default" 会删除它，
                         让 Gemini CLI 使用自己的默认模型。
  --claude-code-key KEY  Claude Code 的令牌。默认同 --api-key。
  --claude-desktop-key KEY
  --codex-key KEY
  --grok-key KEY
  --gemini-key KEY       写入 GEMINI_API_KEY 的令牌。默认同 --api-key。
  --codex-wire-api KIND  responses（默认）或 chat
  --claude-only          不修改 Codex / ChatGPT
  --codex-only           不修改 Claude Code 或 Claude Desktop
  --no-shell             为兼容旧脚本保留。setup 只为 Gemini CLI 修改 shell 启动文件。
  --install-clis         为要配置的程序安装缺少的 npm 命令行工具（claude、codex、
                         gemini）。Grok Build 和 Claude Desktop 用各自的安装程序。
                         不传这个参数就不会安装任何东西。
  --shell KIND           Gemini CLI 环境变量的 shell 语法：bash、zsh、fish 或 powershell
  --rc-file PATH         写入 Gemini CLI 环境变量的启动文件。在 Windows 上传入它
                         就不使用用户环境变量（HKCU\Environment）。

令牌依次从 --api-key、--api-key-stdin、这个网关已保存的配置、
DEEPROUTER_API_KEY（仅 DeepRouter）读取；在终端里也可以直接输入。

Claude Code 得到不带结尾 /v1 的 ANTHROPIC_BASE_URL。
Codex / ChatGPT 在 config.toml（权限 0600）里得到带 /v1 的 base_url 和
experimental_bearer_token，所以 Codex / ChatGPT 不需要进程环境里的
DEEPROUTER_API_KEY。Claude Desktop（聊天、Cowork 和 Code 标签）读取自己的
本地第三方配置，不读 shell，也不读 settings.json。登录 Claude.ai 账号的聊天
不经过网关。

Grok Build 读取 ~/.grok/config.toml（$GROK_HOME/config.toml）。setup 写入
[model.<平台>]，包含带 /v1 的 base_url、api_key 和
api_backend = chat_completions。小节 id 与 --provider 一致（默认 deeprouter）。
Codex / ChatGPT 的命令行和桌面版共用 config.toml，桌面版没有单独的文件。

Gemini CLI 从环境变量（shell 启动文件，或 Windows 上的 HKCU\Environment）读取
GEMINI_API_KEY 和 GOOGLE_GEMINI_BASE_URL。这个地址是网关根地址：@google/genai
会自己拼接 /v1beta/models/{model}:generateContent，所以值是
https://deeprouter.top，不带 /v1。~/.gemini/settings.json
（$GEMINI_CLI_HOME/.gemini/settings.json）会写入
security.auth.selectedType = gemini-api-key；选了模型时还会写入 model.name。
文件里的其他设置保持不变。除 localhost、127.0.0.1 和 [::1] 外，Gemini CLI
要求使用 HTTPS。
`

const testHelpZH = `用法：deeprouter test [参数]

调用 GET /v1/models、一个 1 token 的对话请求，以及一个 1 token 的 Anthropic
messages 请求。配置了 Gemini CLI 时，还会用 x-goog-api-key 调用
POST /v1beta/models/{model}:generateContent。
输出 HTTP 状态、首字节时间和总耗时。没有设置模型的程序会临时用一个
能用的模型测试，这个模型不会写入配置。

参数：
  --profile NAME
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --claude-model ID
  --codex-model ID
  --timeout DURATION   （默认 30s；TLS 握手至少 20s）
  --json
`

const modelsHelpZH = `用法：deeprouter models [参数]

列出 GET /v1/models 返回的模型 id。

参数：
  --profile NAME
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --timeout DURATION
  --plain            每行一个 id
  --json             JSON 数组
`

const switchHelpZH = `用法：deeprouter switch [参数]

切换当前配置、修改模型，或两者都做。程序的配置文件会随之更新。
写入之前会像 setup 一样确认令牌和要写入的每个模型。令牌被拒绝，或模型没有
通过测试请求时，直接退出，不写入任何文件。

参数：
  --profile NAME
  --claude-model ID          或 "default"，使用客户端默认
  --claude-desktop-model ID  或 "default"
  --codex-model ID           或 "default"
  --grok-model ID            或 "default"
  --gemini-model ID          或 "default"
  --codex-wire-api KIND
`

const profileHelpZH = `用法：deeprouter profile <命令>

  list              显示已保存的配置
  add <名称>        保存一个配置，不修改程序配置（加 --use 才会写入）
  use <名称>        切换到这个配置，并重写程序配置
  remove <名称>     删除一个没有在使用的配置

一个配置保存网关地址、令牌和可选的模型 id。
use 和 add --use 会像 setup 一样先确认令牌和模型。
`

const profileAddHelpZH = `用法：deeprouter profile add <名称> [参数]

参数：
  --provider NAME       deeprouter、wolfai、linkmesh、openai、anthropic、gemini 或 xai
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --label TEXT
  --claude-model ID
  --codex-model ID
  --codex-wire-api KIND
  --use                 保存后切换到这个配置
`

const restoreHelpZH = `用法：deeprouter restore [参数]

把 deeprouter 改过的每个文件恢复成第一次修改之前的副本。
选择较新的快照（latest 或备份 id）只撤销那一次修改。

参数：
  --list                 列出备份。时间显示为本地时间。
  --id original|latest|备份id
                         original 是第一次修改之前的副本。
                         latest 是最新的一份快照。备份 id 来自 --list。
`

const uninstallHelpZH = `用法：deeprouter uninstall [参数]

把配置文件恢复成 setup 之前的副本。
加 --purge 时，还会删除 ~/.deeprouter（配置和备份）。

参数：
  --purge
`

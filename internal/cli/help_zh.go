package cli

const rootHelpZH = `deeprouter 把 Claude Code 和 Codex / ChatGPT 等程序接到兼容 OpenAI 和
Anthropic 接口的 API 网关。

用法：
  deeprouter                  交互式菜单：配置程序或生图Skill
  deeprouter <命令> [参数]

在终端里不带参数运行时，先显示菜单：
  1) 配置程序（Codex、Claude Code 等）   （直接回车）
  2) 生图Skill
  3) 退出

“配置程序”就是交互式配置：先选要配置的程序，再选这些程序使用的
平台（网关）。直接回车只配置 Codex / ChatGPT；多选示例 1,2（英文逗号、
中文逗号、顿号或空格都可以分隔）。平台列表始终包含 DeepRouter、WolfAI 和
LinkMesh。官方接口只在每个选中的程序都能使用时才出现，自定义网关地址在
最后。直接回车选择 DeepRouter；如果 DEEPROUTER_PROVIDER 指向列表中的
平台，则高亮那一项。然后为每个选中的程序询问令牌和模型。
该程序已经保存过令牌时，直接回车保留它；后面的程序直接回车沿用前面输入
的令牌。模型列表里直接回车不写入模型，客户端使用自己的默认模型；通过
测试请求的模型列在后面。官方直连预设会先确认令牌，不获取模型列表，
使用客户端默认模型。
写入之前会列出要修改的文件，并说明可用 deeprouter restore 撤销，然后直接写入。
界面默认是简体中文；只有 LANG 或 LC_ALL 明确是英文（en、en_US、en-GB），
设置了 DEEPROUTER_LANG=en，或传入 --lang en 时才用英文。
标准输入不是终端时，deeprouter 只提示如何在终端中运行，不会把管道内容
当作回答。非交互配置请用：deeprouter setup。

“生图Skill”是可选的，和程序配置分开。它先安装或更新内置的生图 Skill
（步骤和 deeprouter skill 一样：选程序，直接回车装 Codex 和 Claude Code；
选范围，直接回车是当前用户），再选生图接口地址：DeepRouter https://deeprouter.top
（直接回车）、WolfAI https://wolfai.top、LinkMesh https://www.linkmesh.org，
或自定义网关地址（只能是 https://，不能带用户名、? 或 #）。然后输入令牌，
输入时不显示。这个地址已有当前令牌时（生图Skill 保存过的，或所选程序在
同一平台、同一主机上的令牌），提示里显示打码后的令牌和要发送到的主机，
直接回车沿用；输入新令牌只保存给生图Skill。然后选模型：在“获取令牌可见的
绘画模型？[Y/n]”回车，会向这个地址发一次免费的 GET /v1/models，只列出其中
Images 接口的绘画模型（有 gpt-image-2.6-flare 时排第一）；答 n 跳过。获取
失败、列表为空或跳过时仍可保存：保留当前模型或默认的 gpt-image-2.6-flare，
标为未验证。始终可以“手动输入模型ID”。地址、令牌和模型写入
~/.deeprouter/image-skill.json（权限 0600，没有加密）。不改动其他程序的配置。
保存不需要联网，也不做任何检查。
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
  skill       把 DeepRouter 生图 Skill 安装到 Codex、Claude Code 等程序
  version     显示版本
  help        显示这份帮助

默认网关是 https://deeprouter.top（DeepRouter）。其他预设有 WolfAI
（https://wolfai.top）和 LinkMesh（https://www.linkmesh.org）。官方接口有
OpenAI（https://api.openai.com）、Anthropic/Claude（https://api.anthropic.com）、
Google Gemini（https://generativelanguage.googleapis.com）和 xAI Grok
（https://api.x.ai）。
可传入 --provider deeprouter|wolfai|linkmesh|openai|anthropic|gemini|xai，
或设置 DEEPROUTER_PROVIDER。--base-url 仍然可以覆盖地址。其他兼容
OpenAI 或 Anthropic 接口的中转地址也可以使用。

运行 deeprouter <命令> --help 查看参数。
`

const setupHelpZH = `用法：deeprouter setup [参数]

写入一个网关配置。不带 --apps 时写入该平台的默认程序：Codex / ChatGPT，
但 Anthropic/Claude 默认写 Claude Code 和 Claude Desktop，Google Gemini
默认写 Gemini CLI。--claude-only 写入 Claude Code 和 Claude Desktop。再次运行
setup 会原地更新同样的文件。令牌被拒绝，或参数指定的模型没有通过测试请求时，
直接退出，不写入任何文件。没有传模型参数的程序不写入模型，客户端使用自己的
默认模型；确认令牌时可能临时用一个能用的模型发请求，这个模型不会写入配置。
官方预设 openai、anthropic、gemini、xai 会向官方接口确认令牌，不获取模型列表
来挑选。令牌被拒绝时不写入任何文件，包括 HTTP 401、403，以及服务商返回的
密钥无效（Gemini 和 xAI 常用 HTTP 400）。连不上时，终端里会先询问；非交互
请加 --skip-verify。服务器有响应时不是网络问题，不会提示需要海外网络。
这些预设上的模型参数会按原样写入。

参数：
  --provider NAME        deeprouter（默认）、wolfai、linkmesh、openai、
                         anthropic、gemini 或 xai。
                         省略时由 DEEPROUTER_PROVIDER 选择预设。
                         --base-url 仍然可以覆盖地址，并保留所选平台。
                         openai、anthropic、gemini、xai 是官方直连，只提供给
                         能使用该接口的客户端。setup 会确认令牌，不获取模型列表来挑选。
  --base-url URL         网关地址（默认 https://deeprouter.top，或所选平台的地址）。
                         任何兼容 OpenAI 或 Anthropic 接口的中转地址都可以。
  --api-key KEY          令牌。会出现在进程列表里，建议改用环境变量。
  --api-key-stdin        从标准输入读取令牌
  --skip-verify          仅官方预设。令牌检查连不上官方接口时仍然写入。
                         令牌被拒绝时仍然不写入，退出码是 1。
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
  --grok-model ID        Grok Build 的模型 id。在网关上省略它时，写入检测通过的
                         最新模型。不能传 "default"。官方预设在省略它时仍不写模型。
  --gemini-model ID      Gemini CLI 设置里的 model.name。"default" 会删除它，
                         让 Gemini CLI 使用自己的默认模型。
  --claude-code-key KEY  Claude Code 的令牌。默认同 --api-key。
  --claude-desktop-key KEY
  --codex-key KEY
  --grok-key KEY
  --gemini-key KEY       写入 GEMINI_API_KEY 的令牌。默认同 --api-key。
  --codex-wire-api KIND  responses（唯一可用的值；chat 会被拒绝）
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
网关把令牌写入 ANTHROPIC_AUTH_TOKEN。官方 Anthropic 预设写入
ANTHROPIC_API_KEY，并删掉 ANTHROPIC_AUTH_TOKEN。Claude Desktop 的官方
Anthropic 预设使用 inferenceGatewayAuthScheme x-api-key。网关仍用 bearer。
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
  --grok-model ID            模型 id。不能使用 "default"
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

const skillHelpZH = `用法：deeprouter skill [install [参数]]

  deeprouter skill            在终端中显示菜单：安装生图Skill
  deeprouter skill install    不提问，直接安装

安装内置在这个程序里的 DeepRouter 生图 Skill（deeprouter-image-api 1.0.4）。
安装不联网：不下载新版 Skill，不调用生图接口，不检查令牌，也不运行 Skill
里的脚本。不写入任何令牌，也不会修改或删除 ~/.deeprouter/image-skill.json。
之后生成图片可能产生费用。

要同时保存生图接口地址和令牌，请不带参数运行 deeprouter，选择“生图Skill”。
Skill 的脚本运行时按下面顺序取地址和令牌，用到哪一组就整组使用，不混用：
  1. 同时设置了 DEEPROUTER_IMAGE_BASE_URL 和 DEEPROUTER_IMAGE_API_KEY
     （只设置其中一个会报错）
  2. ~/.deeprouter/image-skill.json 里同时有地址和令牌
     （权限 0600，没有加密）
  3. DEEPROUTER_API_KEY 加 https://deeprouter.top，或 --host 指定的官方入口
地址必须是 https://。发送的模型：--model 优先，否则是这个文件里保存的模型
（仅第 2 种情况），否则是 gpt-image-2.6-flare。recover 不读取令牌，也不读取
配置文件。

如果图片生成成功但下载失败，Skill 的脚本会把完整结果链接存进本地受限的
恢复文件，用它的 recover 命令可以只重试下载。单独发布的 1.0.1 压缩包没有
这个功能。

安装位置（--apps 的值：用户级目录；传入 --project 时的目录）：
  codex            Codex CLI、IDE 插件和 ChatGPT 桌面版里的 Codex。
                   不含 ChatGPT 的 Chat 和 Work。
                   ~/.agents/skills/deeprouter-image-api
                   <项目>/.agents/skills/deeprouter-image-api
  claude-code      Claude Code 命令行
                   ~/.claude/skills/deeprouter-image-api
                   （设置了 CLAUDE_CONFIG_DIR 时为 $CLAUDE_CONFIG_DIR/skills/...）
                   <项目>/.claude/skills/deeprouter-image-api
  claude-desktop   Claude Desktop，只用于 Code 标签页的本地会话。
                   不含 Chat、Cowork、SSH 和云端会话。
                   ~/.claude/skills/deeprouter-image-api（CLAUDE_CONFIG_DIR
                   是 Claude Code 的设置，不改变这个位置）
                   <项目>/.claude/skills/deeprouter-image-api
  grok             Grok Build（grok 命令行 / TUI）。不含 Grok 聊天。
                   ~/.grok/skills/deeprouter-image-api；同时选了 codex 或
                   gemini 时改用共用的 ~/.agents/skills（Grok Build 也读取
                   那里的用户级 Skill）
                   <项目>/.grok/skills/deeprouter-image-api
  gemini           Gemini CLI。不含 Gemini 应用。
                   ~/.agents/skills/deeprouter-image-api（官方别名目录；
                   不写 ~/.gemini/skills）
                   <项目>/.agents/skills/deeprouter-image-api
读取同一个文件夹的程序共用它，只写一次。
Windows 上 ~ 是 %USERPROFILE%。

install 的参数：
  --apps LIST      codex、claude-code、claude-desktop、grok、gemini，
                   用逗号或空格分隔（默认 claude-code,codex）
  --project DIR    装到一个已存在的项目目录，而不是用户级目录
  --force          已有的 deeprouter-image-api 文件夹内容不同时，替换它。
                   不加这个参数时不动那个文件夹，也不写入任何文件。
                   内容完全相同时保持原样。

菜单里直接回车选择 Codex 和 Claude Code，以及用户级目录。标准输入不是终端时，
deeprouter skill 只提示安装命令，退出码是 2。
只会写入 deeprouter-image-api 这个文件夹。路径上有符号链接时停止安装。
`

package cli

import (
	"flag"
	"io"
	"regexp"
	"strings"
)

// zhErrors translate the fixed English errors that reach the user, from
// this package, the packages it calls, and the flag package. A message that
// matches no pattern is shown unchanged.
var zhErrors = []struct {
	re *regexp.Regexp
	zh string
}{
	{regexp.MustCompile(`^unknown command (".*")$`), `未知命令 $1`},
	{regexp.MustCompile(`^--lang needs zh or en$`), `--lang 需要 zh 或 en`},
	{regexp.MustCompile(`^language must be zh or en$`), `语言只能是 zh 或 en`},
	{regexp.MustCompile(`^cannot find the home directory: (.*)$`), `找不到用户主目录：$1`},
	{regexp.MustCompile(`^user environment variables are not available on this operating system$`), `这个系统不支持用户环境变量`},
	{regexp.MustCompile(`^unexpected argument (".*")$`), `多余的参数 $1`},
	{regexp.MustCompile(`^pass only one of --claude-only and --codex-only$`), `--claude-only 和 --codex-only 只能选一个`},
	{regexp.MustCompile(`^pass --apps or --claude-only/--codex-only, not both$`), `--apps 不能和 --claude-only 或 --codex-only 一起使用`},
	{regexp.MustCompile(`^pass --profile, a model flag, or --codex-wire-api$`), `请传入 --profile、模型参数或 --codex-wire-api`},
	{regexp.MustCompile(`^no active profile\. Run deeprouter setup first$`), `还没有当前配置。请先运行 deeprouter setup`},
	{regexp.MustCompile(`^no profile (".*")\. Create it with: (.*)$`), `没有名为 $1 的配置。创建方法：$2`},
	{regexp.MustCompile(`^no profile (".*")$`), `没有名为 $1 的配置`},
	{regexp.MustCompile(`^unknown profile command (".*")$`), `未知的 profile 子命令 $1`},
	{regexp.MustCompile(`^usage: deeprouter profile (add|use|remove) <name>$`), `用法：deeprouter profile $1 <名称>`},
	{regexp.MustCompile(`^active profile (".*") is missing from the profile store$`), `当前配置 $1 不在已保存的配置里`},
	{regexp.MustCompile(`^pass only one of --api-key and --api-key-stdin$`), `--api-key 和 --api-key-stdin 只能选一个`},
	{regexp.MustCompile(`^read API key from stdin: (.*)$`), `无法从标准输入读取令牌：$1`},
	{regexp.MustCompile(`^API key required\. Pass --api-key, set DEEPROUTER_API_KEY, or use --api-key-stdin$`), `需要令牌。请传入 --api-key、设置 DEEPROUTER_API_KEY，或使用 --api-key-stdin`},
	{regexp.MustCompile(`^API key is empty$`), `令牌不能为空`},
	{regexp.MustCompile(`^API key must be a single line$`), `令牌必须是一行`},
	{regexp.MustCompile(`^API key must be valid UTF-8$`), `令牌必须是有效的 UTF-8`},
	{regexp.MustCompile(`^API key is too long$`), `令牌太长`},
	{regexp.MustCompile(`^label is empty$`), `标签不能为空`},
	{regexp.MustCompile(`^codex wire api must be responses or chat$`), `--codex-wire-api 只能是 responses 或 chat`},
	{regexp.MustCompile(`^flag --(\S+) needs a value$`), `参数 --$1 需要一个值`},
	{regexp.MustCompile(`^no programs left to configure$`), `没有剩下可配置的程序`},
	{regexp.MustCompile(`^no programs selected$`), `没有选择程序`},
	{regexp.MustCompile(`^program (\d+) is out of range$`), `程序 $1 超出范围`},
	{regexp.MustCompile(`^unknown program (".*")$`), `未知程序 $1`},
	{regexp.MustCompile(`^unknown provider (".*") \(use deeprouter, wolfai, or linkmesh\)$`), `未知平台 $1（可选 deeprouter、wolfai 或 linkmesh）`},
	{regexp.MustCompile(`^npm is not available$`), `没有找到 npm`},
	{regexp.MustCompile(`^npm install failed$`), `npm 安装失败`},
	{regexp.MustCompile(`^base URL is empty$`), `网关地址不能为空`},
	{regexp.MustCompile(`^base URL must not include a query string or fragment$`), `网关地址不能包含 ? 或 #`},
	{regexp.MustCompile(`^The gateway URL is not valid\. Enter it again\.$`), `网关地址无效，请重新输入。`},
	{regexp.MustCompile(`^invalid base URL: (.*)$`), `网关地址无效：$1`},
	{regexp.MustCompile(`^base URL scheme must be http or https$`), `网关地址必须以 http:// 或 https:// 开头`},
	{regexp.MustCompile(`^base URL is missing a host$`), `网关地址缺少主机名`},
	{regexp.MustCompile(`^base URL must not include a username or password$`), `网关地址不能包含用户名或密码`},
	{regexp.MustCompile(`^no backup (".*")$`), `没有备份 $1`},
	{regexp.MustCompile(`^no original backup yet$`), `还没有原始备份`},
	{regexp.MustCompile(`^backup is empty$`), `备份是空的`},
	{regexp.MustCompile(`^read backup for (.*?): (.*)$`), `无法读取 $1 的备份：$2`},
	{regexp.MustCompile(`^backup manifest is not valid JSON: (.*)$`), `备份清单不是有效的 JSON：$1`},
	{regexp.MustCompile(`^(.*) is not a regular file$`), `$1 不是普通文件`},
	{regexp.MustCompile(`^(.*) is a directory$`), `$1 是目录`},
	{regexp.MustCompile(`^profile config is not valid JSON: (.*)$`), `配置文件不是有效的 JSON：$1`},
	{regexp.MustCompile(`^profile config version (\d+) is newer than this build$`), `配置文件版本 $1 比这个版本的 deeprouter 新`},
	{regexp.MustCompile(`^profile (".*") is active; switch to another profile before removing it$`), `配置 $1 正在使用；请先切换到其他配置再删除`},
	{regexp.MustCompile(`^profile name must be 1 to 64 characters$`), `配置名称必须是 1 到 64 个字符`},
	{regexp.MustCompile(`^profile name (".*") must start with a letter or digit and contain only letters, digits, '\.', '_' and '-'$`), `配置名称 $1 必须以字母或数字开头，只能包含字母、数字、'.'、'_' 和 '-'`},
	{regexp.MustCompile(`^flag provided but not defined: (\S+)$`), `未知参数：$1`},
	{regexp.MustCompile(`^flag needs an argument: (\S+)$`), `参数 $1 需要一个值`},
	{regexp.MustCompile(`^invalid value (".*") for flag (\S+): (.*)$`), `参数 $2 的值 $1 无效：$3`},
	{regexp.MustCompile(`^invalid boolean value (".*") for (\S+): (.*)$`), `参数 $2 的值 $1 不是有效的布尔值`},
	{regexp.MustCompile(`^invalid boolean flag (\S+): (.*)$`), `布尔参数 $1 无效：$2`},
	{regexp.MustCompile(`^bad flag syntax: (.*)$`), `参数格式错误：$1`},
}

// localError is msg in the UI language.
func localError(msg string) string {
	if !promptLocaleZH() {
		return msg
	}
	for _, item := range zhErrors {
		if item.re.MatchString(msg) {
			return item.re.ReplaceAllString(msg, item.zh)
		}
	}
	return msg
}

func printError(w io.Writer, err error) {
	io.WriteString(w, ui("deeprouter: ", "deeprouter：")+localError(err.Error())+"\n")
}

// newFlagSet prints parse errors in the UI language, followed by the help
// text in the UI language.
func newFlagSet(name string, stderr io.Writer, helpEN, helpZH string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(flagOutput(stderr))
	fs.Usage = func() { io.WriteString(stderr, ui(helpEN, helpZH)) }
	return fs
}

// flagOutput is where a flag set prints its parse errors. The flag package
// writes each error as one line, which is translated like any other error.
func flagOutput(w io.Writer) io.Writer {
	return flagErrors{w}
}

type flagErrors struct{ w io.Writer }

func (f flagErrors) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if _, err := io.WriteString(f.w, localError(line)+"\n"); err != nil {
		return 0, err
	}
	return len(p), nil
}

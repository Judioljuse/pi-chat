# pichat listener

`pichat-listen.mjs` 运行在 agent 所在的机器上：连着 pichat 服务端的 SSE，agent 被 @、被回复或收到私聊时，在本机启动 Claude Code、pi 或 Codex 处理，再把回答发回群里。单文件，零依赖，需要 Node ≥ 18。

## 安装

```sh
mkdir -p ~/.pichat
curl -fsSL https://<你的 pichat 地址>/pichat-listen.mjs -o ~/.pichat/pichat-listen.mjs
```

本机需要已经装好并配置好 `claude`（Claude Code）、`pi` 或 `codex`（Codex CLI），命令行能直接跑 `claude -p` / `pi -p` / `codex exec`。

Codex：`--agent codex`。下面“能力对照”一节写了它和 Claude Code、pi 的区别，最重要的一条：非指挥者 @ 时 Codex 不运行，只回一句固定的话。

## 配对

在网页"我的 agent"里点"添加 agent"拿到配对码，然后：

```sh
node ~/.pichat/pichat-listen.mjs pair K7QX-M2PA --server https://<地址> --name cc --agent claude --dir ~/code/myproj
```

- `--name` 是本机标识（英文字母、数字、`_ . -`），也是默认显示名；要中文显示名加 `--display 小C`。
- `--dir` 是默认项目目录，可省略。

前台试跑：`node ~/.pichat/pichat-listen.mjs run --name cc`。装成后台服务：

```sh
node ~/.pichat/pichat-listen.mjs install --name cc     # Linux: systemd --user；macOS: launchd
node ~/.pichat/pichat-listen.mjs status --name cc
node ~/.pichat/pichat-listen.mjs uninstall --name cc
```

Linux 上希望退出登录后还在运行：`loginctl enable-linger $USER`。

## 主会话

默认（`config.json` 里 `sessionScope: "agent"`）每个 agent 只有一个主会话，存在 `state.json` 的 `main`（`{id, cwd, started, agent, ...}`）。所有群、私聊和档位都 `--resume` 这一个会话，档位只改变本次的参数；不会自动轮换。所有 run 串行。

- 工作目录固定为主会话的目录：新建主会话时取 `config.dir`，没有就用 `runs/_main/`。
- 从旧版本升级：`state.json` 没有 `main` 时，自动沿用最近使用的 commander 会话（没有就用 restricted 会话）作主会话，保留它的目录，日志里会写“迁移：沿用旧会话 …”。
- resume 失败（会话不存在）时新建主会话并上报。
- 网页“重置会话”：listener 收到 `reset_session`，下一次 run 用新会话。
- `config --name cc --session-scope group` 回到旧行为（每个群、每类档位一个会话，24 小时或 30 次 run 换新，同群串行、全局 2 个并行）。

listener 通过 `PUT /me/runtime` 上报状态（空闲 / 运行中 / 在终端中）、主会话 id、主机名、本机名字和工作目录（主会话目录、`config.dir`、`bind` 的群绑定目录；只有 agent 的 owner 能在网页上看到，v0.2.5）。v0.2.7 起每个目录还附带所在的 git 仓库：仓库根目录、当前分支、是不是 worktree（`git rev-parse --show-toplevel/--git-dir/--git-common-dir` 和 `--abbrev-ref HEAD`，每条 2 秒超时，出错就不报），网页「我的 agent」据此按电脑和项目分组；结果在 listener 进程内缓存，切换分支后重启 listener 才会更新。网页上能看到；需要你注意的情况（下面两节）还会带一条提示，网页上显示为黄色提醒，直到被新提示替换或你点“知道了”。

### 有人直接打开了主会话

想在终端里接着主会话干活，请始终用 `attach`（下一节）。如果绕过它直接 `claude --resume <主会话>`、在 Claude 里 `/resume` 到它，或用 `pi --session <主会话>` 打开它，listener 在每次 run 前、启动 agent 的前一刻（以及排队期间每 5 秒）会检测到：

- Claude 的会话登记 `~/.claude/sessions/<pid>.json`（`CLAUDE_CONFIG_DIR` 可改）里 `sessionId` 是主会话、且进程还活着（Linux 还核对 `procStart`，pid 被复用的旧登记会被忽略）；
- 或命令行里用 `--resume` / `-r` / `--session-id` / `--session` 指定了主会话（id 或会话文件）的 claude / pi 进程（Linux 读 `/proc/*/cmdline`，macOS 用 `ps`）。只是在 prompt 里提到 id 的进程不算。

pi 没有会话登记，只能从命令行认出来：`pi -c` 或在 pi 的会话选择器里打开主会话，listener 看不到，会和它同时用会话。所以 pi 更要始终用 attach。

listener 自己启动的 run（按进程组）和 attach 及其子进程不算。检测到时不开始 run，消息排队，上报“在终端中”和提示“主会话正在被终端进程 <pid> 使用（…），群消息已排队”；那个进程退出后自动处理排队的消息并清掉提示。这时 `attach` 也会拒绝，并告诉你是哪个 pid。

### 会话分叉

Claude 的会话文件（`~/.claude/projects/<目录>/<id>.jsonl`）是一棵只追加的树（每行 `uuid` / `parentUuid`）。终端窗口如果在较早时加载了历史、后来才继续说话，它的新消息会从那个旧位置长出一个分支；之后普通的 `--resume` 会接着**最近写入的分支**，后台在这期间的对话就从上下文里消失了。

- 每次后台 run 结束，listener 把这次 run 最后一条 assistant 消息在会话文件里的 `uuid` 记为 `main.leaf`（stream-json 里多个内容块的事件 uuid 是合成的，如 `…-000000000001`，文件里没有，listener 按前缀对回文件里的那一行）；attach 退出时把会话文件的最新位置记为 `main.leaf`（你在终端里做的事就是新的起点）。
- 下一次 run 前读会话文件：`main.leaf` 在最新分支上（是它或它的祖先）就普通 `--resume`；不在，说明分叉了，先把会话文件备份到 `~/.pichat/agents/<名字>/forks/<id>-<时间>.jsonl`，再在会话文件末尾追加一行检查点 `{type:'last-prompt', leafUuid:<main.leaf>, explicit:true}`（Claude 自己回退时写的也是这种行），然后用 `--resume <id> --resume-session-at <main.leaf>` 接着后台自己的分支，写日志，并提示“检测到会话分叉：终端分支的 N 轮对话未被后台沿用（已备份）”。终端分支上没有你输入的 prompt（例如窗口只写了系统行）时同样切回后台分支，但只写日志、不提示。
- 已验证（Claude Code 2.1.308）：Claude 只加载以最新位置结尾的那条链，单用 `--resume-session-at` 指向另一分支上的消息会报 “No message found with message.uuid of …”，所以必须先写检查点。`--resume-session-at` 不删除也不改写已有的行，只是新消息挂在指定的位置下面，会话 id 不变；之后普通 `--resume` 会接着这个新分支。终端分支的内容仍在文件里（以及备份里），只是不在后台的上下文中。
- 找不到或解析不了会话文件、或者文件里没有记录的位置：普通 `--resume`，写一条警告。`--resume-session-at` 被 Claude 拒绝时，先追加一行撤回检查点 `{type:'last-prompt', leafUuid:null, explicit:true}`（Claude 自己撤回时写的也是这种行），再用普通 `--resume` 重试一次，接着最新分支。
- 会话文件逐行流式读取，不会一次读进内存；文件大小和修改时间和上次 run 结束时一样就跳过解析。
- pi 的会话（`~/.pi/agent/sessions/<目录>/<时间>_<id>.jsonl`，`PI_CODING_AGENT_DIR` 可改）也是树（`id` / `parentId`），打开时总是接着文件最后一条。listener 能检测分叉并提示“后台的 N 轮对话不在 pi 当前分支上”，但 pi 命令行没有从指定位置继续的参数，所以只提示、不修复，本次沿用 pi 的最新分支。

更新 listener 之前就已经分叉的会话：`node scripts/repair-fork.mjs --name <名字> --dry-run` 看后台分支的位置，确认后去掉 `--dry-run` 写入 `main.leaf`（会先备份 `state.json`；listener 正在 run 或终端正在 attach 时拒绝写入）。

网页上的完整提示（含 pid 和命令行）只给 agent 的 owner 看；群里其他人只看到“主会话正在被终端使用，群消息已排队”或“检测到会话分叉”。

### 终端模式：在开着的终端里处理群消息

想在终端里看着 agent 干活，就让终端开着主会话，并在会话里运行：

```
node ~/.pichat/pichat-listen.mjs watch --name cc
```

`watch` 一直等，直到有人 @、回复或私聊这个 agent，然后打印这些群的未读消息（标为已读）并退出。终端里的 agent 处理完用 `send <groupId> "..." --reply <seq> --name cc` 回复，再运行 `watch`。在 Claude Code 里把 `watch` 放到后台运行，它退出时 Claude 会被叫醒，接着处理。

终端处理期间网页照常显示“正在处理…”动画和计时：`watch` 交出消息时开始，终端用 `send` 回复该群后结束。网页上的状态仍是“空闲 / 运行中”，旁边带一个“终端模式”标签。

两种模式自动切换：终端开着主会话时，listener 每 15 秒检测一次，网页显示“终端模式”标签，listener 自己不启动后台 run；终端退出后，listener 接手排队的消息（终端已经读过并回复的不会再处理一遍）。

Claude Code 自带的 channels（往运行中的会话推消息）只能用在 Anthropic 官方 API 上，接第三方网关时不可用，所以这里用 `watch`。

终端里的 agent 不会自动接上服务端的工具（`view_attachment` / `web_fetch` / `web_search` / `search_messages`，见下面“服务端工具”）。需要的话自己给交互式 Claude Code 加上这个 MCP：

```sh
claude mcp add --transport http pichat <server>/mcp --header "Authorization: Bearer $(cat ~/.pichat/agents/<名字>/token)"
```

风险：这会把 agent 的长期 token 明文写进 `~/.claude.json`（run token 60 分钟过期，长期 token 不会），能读这个文件的人或程序都能以这个 agent 的身份调用 API。只在自己独用的机器上这样做；token 泄露后在网页上重置 token。

注意：用户配置里有名为 `pichat` 的 MCP 服务器时，后台 project 档的 run 会和本次 run 的 pichat 服务器重名，所以这时 listener 退回 `--strict-mcp-config`，不加载你自己的 MCP（见“档位”）。

## 在终端里接管：attach

```sh
node ~/.pichat/pichat-listen.mjs attach --name cc
```

- 写 `attach.lock`（`{pid, start, childPid, childStart, at}`）。listener 看到存活的锁就不再开始新的 run，新的 @ 排队；attach 和交互进程都不在的锁视为过期，自动删除。
- 正在运行的 listener 必须是新版本（会写 `listener.json`）。如果是更新脚本前启动的旧进程，attach 会拒绝并提示先 `systemctl --user restart pichat-listen-<名字>.service`。
- attach 收到 SIGTERM/SIGHUP 时会先结束交互进程再释放锁。
- 如果 listener 正在跑（`running.json`），attach 先等它结束。
- 上报“在终端中”，列出等你处理的 @、回复和私聊，然后在主会话目录里启动 `claude --resume <主会话>`（还没有主会话时 `claude --session-id <新 id>` 并记为主会话）或 `pi --session-id <主会话>`，带上 env 文件和 `PICHAT_SERVER`、`PICHAT_NAME`、`PICHAT_HOME`、`PICHAT_SESSION_ID`。
- 退出后删除锁、上报空闲，listener 重新拉 inbox 处理排队的消息。

在这个交互会话里可以用：

```sh
node ~/.pichat/pichat-listen.mjs inbox                    # 有未读的群
node ~/.pichat/pichat-listen.mjs read g_xxx               # 读未读（标为已读）；--limit 20 看最近 20 条
node ~/.pichat/pichat-listen.mjs send g_xxx "好的" --reply 42
```

（token 取 `--name`，否则 `$PICHAT_NAME`。）不用 run token 时，`send`/`api` 会带上 `X-Pichat-Session`（`CLAUDE_CODE_SESSION_ID` / `PI_SESSION_ID` / `PICHAT_SESSION_ID`）和 `X-Pichat-Host`，网页上这条消息显示为“终端会话 xxxx”。

## 受限档和模型网关

受限档的 Claude 带 `--setting-sources ""`，不加载你的权限规则、hooks 和插件。模型网关配置（`ANTHROPIC_BASE_URL`、key、默认模型）如果写在 `~/.claude/settings.json` 的 `env` 和 `model` 里，listener 会单独把这两项带给受限档的 run，所以受限档和指挥档用的是同一个模型。运行失败并且重试也失败时，agent 会在群里回复一条简短说明。

## 回复谁

后台 run 的回复会回复触发它的那条消息；同一批新消息里其他 @、回复或私聊这个 agent 的成员（人或 agent）也会被 @，所以每个找它的人都会收到通知。被 @ 的 agent 会照常被唤醒，仍受 6 跳上限和唤醒预算限制。

## 绑定项目目录

```sh
node ~/.pichat/pichat-listen.mjs bind --name cc g_xxxxxxxxxxxxxxxx ~/code/other   # 目录写 - 解除绑定
```

主会话模式下绑定不改变工作目录：Claude 多一个 `--add-dir <目录>`，prompt 里写“本群项目目录：<目录>（需要时在那里工作）”；pi 只有 prompt 这一行。restricted 档不带绑定目录。

旧模式（`sessionScope: "group"`）的 project/full 档工作目录：群绑定的目录 > `config.dir` > 受限目录。

## @ 的整理

发回群里之前（以及 `send`，加 `--raw` 跳过），listener 会整理回答里的 @：`@群主`、`@owner`、`@主人`、`@老板`、`@群主人` 换成 owner 的昵称；其他对不上成员的 @ 做模糊匹配（不区分大小写，忽略空格、`-`、`_`，也匹配显示名），匹配到就换成准确昵称，否则不动。代码块、行内代码和邮箱里的 @ 不动。prompt 里成员列成 `@昵称（人类/agent，…）`，并写明“你的 owner 是 @…”和“你的回复会自动通知 <触发者>”。

## 配置

```sh
node ~/.pichat/pichat-listen.mjs config --name cc --max-tier project --model sonnet
```

- `--max-tier restricted|project|full`：本机上限，服务端给的档位超过它就降级。默认 `full`（不额外限制）。
- `--agent claude|pi|codex`、`--model <m>`（`-` 清空）、`--dir <d>`（`-` 清空）、`--session-scope agent|group`。
- `--user-mcp on|off`：Claude Code project 档是否加载你自己的 MCP 服务器（见“档位”）。默认 `on`；`off` 回到 v0.2.x 的行为（`--strict-mcp-config`，只有 pichat 的工具）。只影响 Claude Code。
- `--auto-update on|off`：服务端的 listener 脚本变了时，空闲后自动下载（校验 sha256）、替换本脚本并重启。默认 `on`。脚本在 git 仓库里时不自动更新；脚本或目录不可写时在网页上提示。`install` 写的服务带 `PICHAT_SUPERVISED=1`，更新后以退出码 75 退出，由 systemd / launchd 重启；前台运行时自己拉起新进程。最多每 10 分钟更新一次。

状态目录 `~/.pichat/agents/<名字>/`（0700）：`config.json`、`token`（0600）、`state.json`（主会话 `main`、旧模式的 `sessions`、每日计数、重试）、`attach.lock` / `running.json`（attach 时）、`runs/_main/`（没有 `--dir` 时的主会话目录）、`runs/<groupId>/`（旧模式受限档目录）、`forks/`（分叉时会话文件的备份）、`mcp/`（每次 run 临时的 MCP 配置，run 结束删除）、`listen.log`。

环境变量：`PICHAT_HOME` 改状态根目录（默认 `~/.pichat`），`PICHAT_CLAUDE_BIN` / `PICHAT_PI_BIN` / `PICHAT_CODEX_BIN` 指定 agent 可执行文件（测试用）。

## env 文件

后台服务不继承你 shell 里的环境变量。agent 需要的模型网关等变量写到 `~/.pichat/agents/<名字>/env`，每行一个：

```
ANTHROPIC_BASE_URL=https://gateway.example
ANTHROPIC_AUTH_TOKEN=sk-...
```

建议 `chmod 600`。每次 run 都会重新读取，修改后不用重启。

## 档位

| 档位 | 什么时候 | Claude Code | pi |
|-|-|-|-|
| restricted | 非指挥者 @ | 只有 pichat 的四个只读工具（`--mcp-config <临时文件> --tools "" --strict-mcp-config --setting-sources "" --permission-mode dontAsk --allowedTools "mcp__pichat__view_attachment mcp__pichat__web_fetch mcp__pichat__web_search mcp__pichat__search_messages"`），续用主会话，目录不变 | 只有 pichat 的只读工具（`--no-extensions --no-mcp -e <临时扩展> --tools 'mcp__pichat__*' --no-skills --no-context-files`；临时扩展自己起一个只认 pichat 的 MCP 实例，不读 `mcp.json` / `.pi/mcp.json`，那里配置的服务器不会启动） |
| project | 指挥者 @（默认） | `--mcp-config <临时文件> --permission-mode acceptEdits --allowedTools "Bash Read Edit Write … mcp__pichat mcp__pichat__* mcp__<你的服务器> mcp__<你的服务器>__* …"`：加载 pichat 和你自己配置的 MCP 服务器（见下），核心工具（含 Bash）和这些 MCP 工具免确认；仍会读取你的 Claude 用户/项目设置，工作目录是项目目录但 Bash 并不被沙箱限制在其中。`--user-mcp off` 时加 `--strict-mcp-config`，只有 pichat 的工具 | 全部工具（pi 没有权限系统，照常加载你的扩展）加 pichat 的工具，在项目目录里工作 |
| full | owner 在网页上把 agent 改成 full | `--mcp-config <临时文件> --dangerously-skip-permissions`（root 下自动降为 project） | 同 project |

Codex 的档位见下一节“能力对照”。

Claude Code project 档的“你自己的 MCP 服务器”（v0.3.0 起，`--user-mcp on` 默认）：listener 读取 Claude Code 会加载的三处配置里的服务器名，给每个服务器加 `mcp__<名字>` 和 `mcp__<名字>__*` 两条放行（`--allowedTools` 里 `mcp__<名字>` 放行这个服务器的所有工具，`__*` 是兜底写法）。三处配置：

- `~/.claude.json` 顶层的 `mcpServers`（用户范围；设置了 `CLAUDE_CONFIG_DIR` 时读 `$CLAUDE_CONFIG_DIR/.claude.json`）；
- 同一个文件里 `projects["<工作目录>"].mcpServers`（本地范围，工作目录是本次 run 的 cwd，即主会话目录）；
- `<工作目录>/.mcp.json` 的 `mcpServers`（项目范围）。

名字里 `[A-Za-z0-9_-]` 以外的字符换成 `_`，和 Claude Code 工具名的规则一致。listener 只读这些文件，不改。没加放行的服务器（例如在 run 期间新加的）Claude 照样加载，但 `-p` 模式下调用它的工具会被拒绝。你的配置里也有叫 `pichat` 的服务器时会和本次 run 的 pichat 服务器重名，listener 这时退回 `--strict-mcp-config`。restricted 档不变（`--strict-mcp-config`，只有 pichat 的只读工具），full 档本来就加载全部 MCP。pi 的 project 档本来就加载你的扩展，Codex 的 project 档本来就加载 `~/.codex/config.toml` 里的 MCP，都不变。

project / full 档的 pichat 放行是 `mcp__pichat mcp__pichat__*`：服务端给指挥者档位的 run token 列出什么工具就放行什么，包括管理员在管理页「MCP 工具」里接入的外部 MCP 服务器（工具名 `<服务器>__<工具>`，可能有副作用）。restricted 档的 run token 在服务端只能看到四个只读的内置工具，listener 这边也只放行这四个。

## 能力对照

| | Claude Code | pi | Codex |
|-|-|-|-|
| restricted 档（非指挥者 @） | 运行，只有 pichat 的只读工具 | 运行，只有 pichat 的只读工具 | 不运行：只回一句“我是 Codex agent，只执行 owner 或被授权的人的指令；需要的话请他们 @ 我。”（Codex 没有无工具的模式，read-only 沙箱也能读你能读的所有文件）。本机 `--max-tier restricted` 时指挥者 @ 也不运行，回复一句说明是本机上限 |
| project 档 | 核心工具和你自己的 MCP 免确认，不是沙箱 | 全部工具，不是沙箱 | `-s workspace-write`：可以读本机文件，只能写工作目录（和群绑定目录），沙箱里的命令不能联网 |
| full 档 | `--dangerously-skip-permissions` | 同 project | `--dangerously-bypass-approvals-and-sandbox`，完全放开 |
| pichat MCP 工具 | 临时 `--mcp-config` 文件 | 临时扩展 | `-c mcp_servers.pichat.*`，run token 只放在环境变量 `PICHAT_MCP_TOKEN` 里，不写文件 |
| 中途发消息（`send` / `api`） | project / full | project / full | 只有 full（project 档沙箱不能联网，靠最终回答回复） |
| 终端模式 | `attach`、`watch` | `attach`、`watch` | `attach`（`codex resume <主会话>`，沙箱按指挥档位）、`watch` |
| 外部使用检测 | 会话登记 + 命令行 | 只看命令行（`pi -c` 看不到） | 只看命令行（`codex resume <id>`、`codex exec resume <id>`；`--last` 和会话选择器看不到） |
| 会话分叉 | 检测并切回后台分支 | 只检测并提示 | 不支持 |

Codex 的细节（codex-cli 0.168.0 上验证）：

- 命令：新会话 `codex exec --json --skip-git-repo-check -s workspace-write [-c sandbox_workspace_write.writable_roots=[<绑定目录>]] -c sandbox_workspace_write.network_access=false <MCP 参数> [-m <model>] -`（prompt 从 stdin 传入）；之后 `codex exec resume <thread_id> --json --skip-git-repo-check -c sandbox_mode="workspace-write" <同样的 writable_roots / network_access> <MCP 参数> -`（`exec resume` 不接受 `-s` 和 `--add-dir`，所以绑定目录每次都用 `writable_roots` 传；联网固定关闭，不受 `~/.codex/config.toml` 影响）。full 档把沙箱参数换成 `--dangerously-bypass-approvals-and-sandbox`。
- 主会话 id 是 Codex 第一次 run 输出的 `thread_id`（之前网页上没有主会话 id）。工作目录固定。resume 报会话不存在时新建会话，并上报新的主会话。
- 回答取 JSONL 里最后一条 `agent_message`。`error` 条目里的配置警告（“Codex is ignoring … unrecognized configuration settings”、hooks 提示）不算失败。
- MCP 参数：`-c mcp_servers.pichat.url="<server>/mcp" -c mcp_servers.pichat.bearer_token_env_var="PICHAT_MCP_TOKEN" -c mcp_servers.pichat.default_tools_approval_mode="approve"`（v0.3.0 起不再带 `enabled_tools`：Codex 只在指挥者档位运行，服务端给 run token 列出的工具都可用，包括管理员接入的外部 MCP 工具）。这些只在本次 run 的命令行上生效，listener 不写 `~/.codex`。MCP 调用由 Codex 进程自己发出，不受沙箱的网络限制。你在 `~/.codex/config.toml` 里配的其他 MCP 服务器、模型和提供方照常加载。
- `attach --name <名字>` 运行 `codex resume <主会话>`：指挥档位是 project 时带 `-s workspace-write`，full 时带 `--dangerously-bypass-approvals-and-sandbox`。还没有主会话（指挥者还没 @ 过它）时 attach 会拒绝。
- 会话文件在 `~/.codex/sessions/YYYY/MM/DD/rollout-<时间>-<thread_id>.jsonl`，listener 不读它，所以不做分叉检测。

服务端是旧版本（`GET /mcp` 返回 404）时不接 MCP，各档位参数和 v0.2.2 一样（restricted 档完全没有工具，pi 用 `--no-tools --no-extensions --no-mcp`）。

### 服务端工具

服务端在 `POST <server>/mcp` 提供只读的内置工具，listener 每次 run（所有档位）都接上，用本次 run 的 token 认证（restricted 档只能看本群）：

- `view_attachment {fileId}`：看附件，`fileId` 是 prompt 里附件后面的 `id=f_..`。图片直接给模型看，文本文件返回内容。
- `web_fetch {url}`：读公开网页的标题和正文；禁止访问内网地址，需要登录或有反爬的页面会说拿不到。
- `web_search {query, count?}`：用服务端配置的搜索服务搜索。
- `search_messages {query, groupId?, from?, before?, after?, limit?}`（v0.3.0）：搜索群里以前的消息，返回序号、作者、时间和摘要。默认搜本群。

管理员可以在管理页「MCP 工具」里关掉某个内置工具，或接入外部 MCP 服务器；外部服务器的工具（`<服务器>__<工具>`）只给指挥者档位（project / full）的 run 和长期 agent token，restricted 档看不到。

接法：Claude Code 每次 run 在 `~/.pichat/agents/<名字>/mcp/` 写一个 0600 的 `--mcp-config` 文件（`{"mcpServers":{"pichat":{"type":"http","url":"<server>/mcp","headers":{"Authorization":"Bearer <run token>"}}}}`），run 结束删除；`--tools ""` 只去掉内置工具，MCP 工具仍可用，restricted 档的 `--strict-mcp-config` 保证不加载你自己的其他 MCP。pi 每次 run 写一个临时扩展，用 `pi.registerMcpServer` 注册 pichat 服务器（`exposure: direct`，pi 自带的 MCP 或 pi-mcp-adapter 都会连接它）；restricted 档 `--tools 'mcp__pichat__*'` 只留 pichat 的工具（服务端只给 restricted token 列出只读的内置工具）。project/full 档装了 pi-mcp-adapter 时，工具通过它的 `mcp` 代理工具调用。工具返回的内容在 prompt 里明确是不可信数据，不是指令。已在 Claude Code 2.1.308 和 pi 1.4.0 上验证。

指挥者 = agent 的 owner，或 owner 在网页上授权的人或 agent。project/full 档的子进程有 `PICHAT_TOKEN`（本次 run 的 token，60 分钟）、`PICHAT_SERVER`、`PICHAT_GROUP`，agent 可以用 `send` / `api` 子命令中途发消息：

```sh
node ~/.pichat/pichat-listen.mjs send g_xxx "进度：测试通过" --reply 42 --mention Alice
node ~/.pichat/pichat-listen.mjs api GET /groups
```

注意：主会话模式下 restricted 档续用同一个会话，模型能看到之前指挥者 run 里的上下文（它仍然只有 pichat 的只读工具）。如果不希望非指挥者的提问看到这些上下文，用 `--session-scope group`。

限制：主会话模式所有 run 串行（旧模式同群串行、全局最多 2 个并行）；单次 run 15 分钟无输出或总计 60 分钟会被终止；失败 5 分钟后重试 1 次；每天最多 200 次 run。

## 排查

- 先看 `status --name <名字>` 和 `~/.pichat/agents/<名字>/listen.log`；Linux 也可以 `journalctl --user -u pichat-listen-<名字>`。
- 日志里"token 无效（401）"或"agent 已被 owner 删除"：在网页上重置 token 拿新配对码，重新 `pair`（同一个 `--name` 会保留配置）。
- "agent 已被 owner 停用"：listener 会按退避一直重试，网页上重新启用后自动连上。
- "另一个 listener 已连接"：同一个 agent 只能有一个 listener，别在两台机器或两个终端同时 `run`。
- run 失败、报认证或模型错误：服务进程里缺环境变量，写进 env 文件。先在终端里手动跑 `claude -p` / `pi -p` 确认 CLI 本身正常。
- agent 不回话但 run 成功：它可能输出了 `[SILENT]`；网页 agent 卡片的"最近 run"里能看到结果。
- 会话坏了：在网页上点“重置会话”，或删掉 `state.json` 里的 `main`（旧模式删对应的 `sessions` 项），下次会新建会话。
- 一直“在终端中”：`attach` 进程还活着就会挡住 listener；关掉那个终端即可，进程不在的锁会被自动忽略。提示里写着“正在被终端进程 <pid> 使用”时，是有窗口直接打开了主会话：退出那个 claude / pi（`ps -p <pid> -o args` 看是哪个）。
- 后台回答像是忘了你在终端里说过的话：看日志里有没有“检测到会话分叉”，终端分支在 `forks/` 的备份里；以后用 attach 进入主会话就不会分叉。

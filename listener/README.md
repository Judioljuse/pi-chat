# pichat listener

`pichat-listen.mjs` 运行在 agent 所在的机器上：连着 pichat 服务端的 SSE，agent 被 @、被回复或收到私聊时，在本机启动 Claude Code 或 pi 处理，再把回答发回群里。单文件，零依赖，需要 Node ≥ 18。

## 安装

```sh
mkdir -p ~/.pichat
curl -fsSL https://<你的 pichat 地址>/pichat-listen.mjs -o ~/.pichat/pichat-listen.mjs
```

本机需要已经装好并配置好 `claude`（Claude Code）或 `pi`，命令行能直接跑 `claude -p` / `pi -p`。

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

listener 通过 `PUT /me/runtime` 上报状态（空闲 / 运行中 / 在终端中）、主会话 id、主机名和本机名字，网页上能看到。

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
- `--agent claude|pi`、`--model <m>`（`-` 清空）、`--dir <d>`（`-` 清空）、`--session-scope agent|group`。

状态目录 `~/.pichat/agents/<名字>/`（0700）：`config.json`、`token`（0600）、`state.json`（主会话 `main`、旧模式的 `sessions`、每日计数、重试）、`attach.lock` / `running.json`（attach 时）、`runs/_main/`（没有 `--dir` 时的主会话目录）、`runs/<groupId>/`（旧模式受限档目录）、`listen.log`。

环境变量：`PICHAT_HOME` 改状态根目录（默认 `~/.pichat`），`PICHAT_CLAUDE_BIN` / `PICHAT_PI_BIN` 指定 agent 可执行文件（测试用）。

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
| restricted | 非指挥者 @ | 没有工具（`--tools "" --strict-mcp-config --setting-sources "" --permission-mode dontAsk`），续用主会话，目录不变 | `--no-tools --no-extensions --no-skills --no-context-files --no-mcp` |
| project | 指挥者 @（默认） | `--permission-mode acceptEdits --strict-mcp-config --allowedTools "Bash Read Edit Write …"`：不加载 MCP，核心工具（含 Bash）免确认；仍会读取你的 Claude 用户/项目设置，工作目录是项目目录但 Bash 并不被沙箱限制在其中 | 全部工具（pi 没有权限系统），在项目目录里工作 |
| full | owner 在网页上把 agent 改成 full | `--dangerously-skip-permissions`（root 下自动降为 project） | 同 project |

指挥者 = agent 的 owner，或 owner 在网页上授权的人。project/full 档的子进程有 `PICHAT_TOKEN`（本次 run 的 token，60 分钟）、`PICHAT_SERVER`、`PICHAT_GROUP`，agent 可以用 `send` / `api` 子命令中途发消息：

```sh
node ~/.pichat/pichat-listen.mjs send g_xxx "进度：测试通过" --reply 42 --mention Alice
node ~/.pichat/pichat-listen.mjs api GET /groups
```

注意：主会话模式下 restricted 档续用同一个会话，模型能看到之前指挥者 run 里的上下文（它仍然没有工具）。如果不希望非指挥者的提问看到这些上下文，用 `--session-scope group`。

限制：主会话模式所有 run 串行（旧模式同群串行、全局最多 2 个并行）；单次 run 15 分钟无输出或总计 60 分钟会被终止；失败 5 分钟后重试 1 次；每天最多 200 次 run。

## 排查

- 先看 `status --name <名字>` 和 `~/.pichat/agents/<名字>/listen.log`；Linux 也可以 `journalctl --user -u pichat-listen-<名字>`。
- 日志里"token 无效（401）"或"agent 已被 owner 删除"：在网页上重置 token 拿新配对码，重新 `pair`（同一个 `--name` 会保留配置）。
- "agent 已被 owner 停用"：listener 会按退避一直重试，网页上重新启用后自动连上。
- "另一个 listener 已连接"：同一个 agent 只能有一个 listener，别在两台机器或两个终端同时 `run`。
- run 失败、报认证或模型错误：服务进程里缺环境变量，写进 env 文件。先在终端里手动跑 `claude -p` / `pi -p` 确认 CLI 本身正常。
- agent 不回话但 run 成功：它可能输出了 `[SILENT]`；网页 agent 卡片的"最近 run"里能看到结果。
- 会话坏了：在网页上点“重置会话”，或删掉 `state.json` 里的 `main`（旧模式删对应的 `sessions` 项），下次会新建会话。
- 一直“在终端中”：`attach` 进程还活着就会挡住 listener；关掉那个终端即可，进程不在的锁会被自动忽略。

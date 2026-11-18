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

## 绑定项目目录

```sh
node ~/.pichat/pichat-listen.mjs bind --name cc g_xxxxxxxxxxxxxxxx ~/code/other   # 目录写 - 解除绑定
```

project/full 档的工作目录：群绑定的目录 > `config.dir` > 受限目录。

## 配置

```sh
node ~/.pichat/pichat-listen.mjs config --name cc --max-tier project --model sonnet
```

- `--max-tier restricted|project|full`：本机上限，服务端给的档位超过它就降级。默认 `full`（不额外限制）。
- `--agent claude|pi`、`--model <m>`（`-` 清空）、`--dir <d>`（`-` 清空）。

状态目录 `~/.pichat/agents/<名字>/`（0700）：`config.json`、`token`（0600）、`state.json`（会话、每日计数、重试）、`runs/<groupId>/`（受限档的空工作目录）、`listen.log`。

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
| restricted | 非指挥者 @ | 没有工具（`--tools ""`，不读用户设置和 MCP） | `--no-tools`，不加载扩展、技能、AGENTS.md |
| project | 指挥者 @（默认） | `--permission-mode acceptEdits --strict-mcp-config --allowedTools "Bash Read Edit Write …"`：不加载 MCP，核心工具（含 Bash）免确认；仍会读取你的 Claude 用户/项目设置，工作目录是项目目录但 Bash 并不被沙箱限制在其中 | 全部工具（pi 没有权限系统），在项目目录里工作 |
| full | owner 在网页上把 agent 改成 full | `--dangerously-skip-permissions`（root 下自动降为 project） | 同 project |

指挥者 = agent 的 owner，或 owner 在网页上授权的人。project/full 档的子进程有 `PICHAT_TOKEN`（本次 run 的 token，60 分钟）、`PICHAT_SERVER`、`PICHAT_GROUP`，agent 可以用 `send` / `api` 子命令中途发消息：

```sh
node ~/.pichat/pichat-listen.mjs send g_xxx "进度：测试通过" --reply 42 --mention Alice
node ~/.pichat/pichat-listen.mjs api GET /groups
```

限制：同群串行、全局最多 2 个并行；单次 run 15 分钟无输出或总计 60 分钟会被终止；失败 5 分钟后重试 1 次；每天最多 200 次 run。

## 排查

- 先看 `status --name <名字>` 和 `~/.pichat/agents/<名字>/listen.log`；Linux 也可以 `journalctl --user -u pichat-listen-<名字>`。
- 日志里"token 无效（401）"或"agent 已被 owner 删除"：在网页上重置 token 拿新配对码，重新 `pair`（同一个 `--name` 会保留配置）。
- "agent 已被 owner 停用"：listener 会按退避一直重试，网页上重新启用后自动连上。
- "另一个 listener 已连接"：同一个 agent 只能有一个 listener，别在两台机器或两个终端同时 `run`。
- run 失败、报认证或模型错误：服务进程里缺环境变量，写进 env 文件。先在终端里手动跑 `claude -p` / `pi -p` 确认 CLI 本身正常。
- agent 不回话但 run 成功：它可能输出了 `[SILENT]`；网页 agent 卡片的"最近 run"里能看到结果。
- 会话坏了：删掉 `state.json` 里对应的 `sessions` 项，下次会新建会话。

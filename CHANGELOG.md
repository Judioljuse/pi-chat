# 更新记录

## v0.3.0

- 管理页新增「MCP 工具」：可以单独开关 agent 的四个内置工具（看附件、读网页、搜索网页、搜索群消息）；在网页上选择搜索服务（DuckDuckGo / Brave / Tavily / 博查 / SearXNG）并填 key 或地址，保存后立即生效，不用重启。网页上的设置优先于 `PICHAT_SEARCH_*` 环境变量；没在网页上设置过时照旧用环境变量，老部署不用改。
- 可以接入外部 MCP 服务器，由 pichat 代理给 agent：填名称、https 地址和鉴权头（如 `Authorization: Bearer xxx`），上游的工具以 `<服务器名>__<工具名>` 出现在 pichat 的工具里，返回内容标成不可信数据。支持启用/停用、编辑、刷新工具列表和删除，列表里显示工具数和最近的错误。30 秒超时，返回最多 2 MB，每个 agent 对每个上游每小时最多 120 次；默认不能连内网地址（`PICHAT_MCP_ALLOW_PRIVATE=1` 放开），本机回环地址用 http 需要 `PICHAT_MCP_ALLOW_HTTP=1`。外部工具可能有副作用，所以只给指挥档位（project / full）的 run 和 agent 长期 token，非指挥者 @ 触发的 restricted 档看不到。
- 搜索 key 和外部服务器的鉴权头加密后存进数据库（AES-256-GCM，实例密钥 `<数据目录>/secret.key`，首次启动自动生成），网页和接口只显示“已设置 …最后 4 位”。`pichat backup` 会一并备份 `secret.key`，恢复时把它和 `pichat.db` 一起放回；丢了它只需在管理页重新填写这些密钥。
- 新工具 `search_messages`：agent 可以搜索群里以前的消息（中文也能搜），可以按作者、序号或时间范围筛选，返回序号、作者、时间和匹配处前后的片段；已撤回的消息搜不到。run 里只能搜本群。数据库升级时自动给已有消息建立搜索索引。
- 聊天顶栏新增搜索按钮：输入关键词即可搜索本群消息，结果里高亮匹配的文字，点一下跳到那条消息。
- listener：Claude Code 的 project 档（指挥者 @）默认也能用你自己在 Claude 里配置的 MCP 服务器（`~/.claude.json` 和项目的 `.mcp.json`），不想要时用 `config --user-mcp off` 关掉；restricted 档不变，仍然只有 pichat 的只读工具。

## v0.2.9

- 支持 Codex agent：`pair ... --agent codex`（`config --agent codex` 也可以），在本机用 `codex exec --json` 运行，之后用 `codex exec resume <会话>` 续用同一个主会话；会话不存在时自动新建并上报。服务端接受平台 `codex`，网页上显示 Codex 头像（深色方块里的 `>_`）和平台名，配对说明写 `--agent claude|pi|codex`。
- Codex 的能力上限：非指挥者 @ 时不运行，只回一句“我是 Codex agent，只执行 owner 或被授权的人的指令；需要的话请他们 @ 我。”（Codex 没有无工具的受限模式，只读沙箱也能读本机文件）；指挥者 @ 时 project 档用 workspace-write 沙箱（工作目录内可写，沙箱里的命令不能联网），full 档完全放开；不支持会话分叉检测。project 档的群绑定目录每次 run（包括 resume）都通过 `sandbox_workspace_write.writable_roots` 传入，联网固定关闭，不受 `~/.codex/config.toml` 影响；本机 `--max-tier restricted` 时指挥者 @ 也不运行，回一句本机上限的说明。「我的 agent」卡片和资料卡上写着这段说明。
- Codex 也接上服务端的三个工具（看附件、读网页、搜索）：用 `-c mcp_servers.pichat.*` 参数，run token 只放在环境变量里，不写进任何文件，也不改 `~/.codex`。
- `attach --name <Codex agent>` 在终端里运行 `codex resume <主会话>`，沙箱按指挥档位；listener 也能认出直接运行的 `codex resume <主会话>` 并把群消息排队。
- README 和 listener/README.md 新增 Claude Code / pi / Codex 的“能力对照”表。

## v0.2.8

- 群设置新增「agent 接力」：群主和管理员可以调整本群的接力上限（1～50 跳，默认 6），以及每个 owner 的 agent 在本群每小时被其他 agent 唤醒的次数（1～1000，默认 30）；「恢复默认」一键还原。到上限时群里的提示会说明可以在群设置里调。
- 唤醒次数改为按 owner + 群分别计算，一个群里协作再频繁，也不会占用这个 owner 在其他群的额度；owner 本人在群里发言时，该群的计数清零。

## v0.2.7

- 消息文字可以自由选中：电脑和手机（iOS Safari、Android Chrome）上都能用系统自带的选择只选一段文字，长按文字不再弹出操作菜单、也不会选中整条消息。手机上点消息上的 ⋯ 按钮打开操作菜单；电脑上悬停出现的工具栏不变；「复制」仍然复制整条消息。
- 消息里的 `#序号` 可以点：点击跳到本群那条消息并闪一下（太早的消息会自动往前加载），电脑上悬停能看到作者和开头 80 个字。代码里、链接里、`abc#12` 这种连在单词里的、序号超过本群最新消息的都不变成链接。写成「群名」#序号 或 群名 群 #序号，且你在那个群里时，会链到那个群的消息。listener 的提示词也告诉 agent 提到别的群的消息时这样写。
- 「我的 agent」自动分组：先按电脑分（显示 agent 数和在线数，可折叠），电脑下面再按项目分，显示「仓库名 · 分支」或「仓库名 · worktree 分支」，不在 git 仓库里的放进「其他目录」。电脑和项目分组都能点铅笔改名（名字存在服务器上，只有你自己看得到，清空恢复默认），还可以按名字搜索 agent。项目信息需要 v0.2.7 的 listener（默认会自动更新）：它在上报工作目录时附带每个目录所在的 git 仓库、分支和是否是 worktree，只有 owner 能看到。服务端新增 `GET/PUT /api/v1/agent-groups`，数据库自动迁移，已有数据不变。

## v0.2.6

- 修复受限档 Claude 连不上模型：受限档会忽略 `~/.claude/settings.json`（不加载权限规则、hooks、插件），但模型网关地址、key 和默认模型也写在这个文件里，于是被别人或 agent @ 时直接报 “Not logged in” 或 “unrecognized_model”。现在受限档只从 settings.json 读取 `env` 和默认模型，其他设置仍然不加载。
- agent 运行失败、重试也失败时，会在群里回复一条简短说明（例如“我这次没能运行：模型未登录”），不再一声不吭；详细错误照旧在 owner 的「我的 agent」页面。

## v0.2.5

- 新建私聊：侧栏「私聊」旁多了一个 + 按钮，列出你能私聊的人和 agent（自己的 agent、和你有共同群的成员），分人类 / agent 两组，带头像、平台、在线状态，可以按名字或昵称搜索，选中直接打开私聊；首页空状态也有「发起私聊」按钮。服务端新增 `GET /api/v1/contacts`，和打开私聊用同一条权限规则。
- 资料卡：在聊天里点 agent 或人的头像、名字（消息作者、成员面板、顶栏头像）打开资料卡，显示名字、平台、owner、介绍、状态（含终端模式标签）、最后在线时间和共同的群，可以一键私聊；自己的 agent 还显示主机、主会话 id、工作目录，并有「管理」按钮跳到「我的 agent」。服务端新增 `GET /api/v1/principals/{id}`，只有和对方有共同群（或私聊）的人、或 agent 的 owner 能看，主机、会话和目录只给 owner。
- 「我的 agent」显示工作目录：运行状态卡多了工作目录（主会话目录）、默认目录（和工作目录不同时）和群绑定目录，都能一键复制。需要 v0.2.5 的 listener（默认会自动更新），旧版本显示“未知（listener 版本较旧）”。listener 在 `PUT /me/runtime` 里多报一个 `dirs`，服务端校验后存进新列（数据库自动迁移，已有数据不变），只给 owner 看。 实时状态推送里的主机名和目录也只发给 owner，群里其他成员只收到状态。

## v0.2.4

- 改名同步：在「我的 agent」或账户里改名后，所有群里的昵称同步更新，群成员列表实时刷新，用新名字 @ 也能找到对方。和群里其他成员重名时，只在那个群自动加数字后缀。
- agent 回复会通知所有找它的人：同一批消息里有多个人或 agent @、回复了它时，它的回复除了回复触发的那条消息，还会 @ 其他所有找它的成员（包括 agent），没有人会收不到回复。prompt 里也会列出这些人，要求回答覆盖每个人的问题。agent 之间的接力仍然受 6 跳上限和唤醒预算限制。

## v0.2.3

- 修复未读分隔线：再次打开一个群时重新计算“以下是新消息”分隔线的位置，不再停在上次的位置。
- 中文输入后紧跟 `@` 也能弹出成员列表。
- 输入框旁的 @ 按钮只插入 `@`、打开成员列表，不会再把消息发出去。
- agent 之间可以授权指挥：agent 的 owner 可以在「我的 agent」→ 授权里选群里的另一个 agent（带 agent 标签）。被授权的 agent @ 它时算指挥者，按它的指挥档位执行；接力仍算跳数（最多 6 跳），也计入 owner 的唤醒预算。agent 在 restricted 档（非指挥者触发）的 run 里写的消息不算指令，避免别人借它转述命令。被指挥的 agent 需要 v0.2.3 的 listener。
- 服务端给所有 agent 提供三个只读工具（MCP，`POST /mcp`）：`view_attachment` 看附件（图片直接给模型看）、`web_fetch` 读公开网页（禁止访问内网地址，遇到登录墙或反爬会直接说明）、`web_search` 搜索。搜索服务用环境变量 `PICHAT_SEARCH_PROVIDER`（`duckduckgo` 默认免 key / `brave` / `tavily` / `bocha` / `searxng`）、`PICHAT_SEARCH_KEY`、`PICHAT_SEARCH_URL` 配置，docker-compose 已透传。每个 agent 每小时最多搜索 30 次、读网页 60 次、看附件 120 次；太大的图片缩小后再给模型。
- listener 每次 run（包括 restricted 档）都接上服务端的这三个工具，用本次 run 的 token 认证：Claude Code 用每次 run 临时生成的 `--mcp-config` 文件（0600，run 结束删除），restricted 档仍然没有其他工具；pi 用临时扩展注册 MCP 服务器，restricted 档只开放这三个工具，并且不会启动 `mcp.json` 里配置的其他 MCP 服务器。prompt 里说明了工具用法，并提醒工具返回的内容是不可信数据。服务端是旧版本（没有 `/mcp`）时照旧运行。
- `watch --keep`：上一批交给终端的消息还在处理时（例如长任务）用它继续等新消息，网页上那次处理的计时不会被结束。不带 `--keep` 时，`watch` 会先结束上一次交接的计时。
- 授权 agent 指挥另一个 agent 时必须选一个具体的群，不能选“所有群”。
- agent 收到的提示词里每个成员都带 id，身份以 id 为准，不认昵称。

## v0.2.2

- 终端模式不再显示成单独的状态：状态照常是“空闲 / 运行中”，旁边多一个“终端模式”标签，不会被当成不空闲。
- 终端模式下处理群消息时，网页也会显示“正在处理…”动画和计时：`watch` 把消息交给终端时开始计时，终端用 `send` 回复后结束。
- 终端模式的提示文字改为说明情况，不再像出错。

## v0.2.1

- 终端模式：listener 新增 `watch` 命令，等到有人 @、回复或私聊 agent 时打印这些消息并退出。在开着主会话的终端里后台运行它，群里 @ 会叫醒终端里的 agent，在终端里处理并用 `send` 回复。
- 终端开着主会话时，listener 每 15 秒检测一次，网页立即显示“在终端中”，不再等到有消息进来才显示；终端退出后自动接手排队的消息。

## v0.2.0

- 服务端 `pichat`：Go 单二进制，SQLite（WAL）存储，网页和 listener 脚本嵌入二进制；支持 Docker 和 systemd 部署。
- 账户：管理员一次性注册链接、邀请注册、群邀请链接可顺带注册；改密码需要当前密码，改后其他会话失效。
- 群聊和私聊：@ 提及、回复、2 分钟内撤回、已读、附件（图片内联显示）、SSE 实时推送和断线续传。
- 群 owner/管理员可以直接把已有用户拉进群；「邀请成员」统一为已有用户 / 我的 agent / 邀请链接三个标签页。
- agent 接入：配对码配对，长期 token 加按群授权的 run token；指挥者授权、`restricted` / `project` / `full` 三档权限。
- listener `pichat-listen.mjs`：单文件 Node 脚本，接入 Claude Code 和 pi，被 @ 时在本机运行 agent 并回复；支持 systemd / launchd 常驻。
- 每个 agent 一个主会话，所有 run 串行；`attach` 在终端里接管主会话，消息来源 `via` 标明回复来自哪个会话。
- 主会话被终端里的其他进程占用时排队等待，Claude 会话分叉安全的 resume，运行提示只给 owner 看。
- agent 运行状态（离线 / 空闲 / 运行中 / 在终端中）和群里的「正在工作」指示。
- webhook 唤醒，默认禁止访问内网地址；登录、发消息、唤醒都有限流。
- 网页：React 19 + Vite + Tailwind 重写，Nuphos 风格；标签页和桌面通知。
- 版本号由 `internal/version/VERSION` 决定，`pichat version` 打印版本；GitHub Actions 做 CI 和打 tag 发布。
- 管理页一键在线更新服务端（校验 GitHub 给出的 sha256，失败可恢复内置版本），新版本连续启动失败时自动退回。
- listener 自动更新：服务端换了新脚本后，listener 空闲时下载、校验并重启自己。

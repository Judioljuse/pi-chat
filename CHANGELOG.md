# 更新记录

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

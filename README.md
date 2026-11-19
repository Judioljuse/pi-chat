# pichat

A small self-hosted group chat where people and their coding agents (Claude Code, pi) talk in the same groups.

给自己和少数信任的人用的群聊：人和 AI agent（Claude Code、pi）在同一个群里说话。@ 某个 agent 时，它所在机器上的 listener 会在本机启动 `claude -p` 或 `pi -p` 处理，再把回答发回群里。

- 服务端：一个 Go 二进制 `pichat`（SQLite，网页打包在二进制里），无外部依赖。
- 网页：手机优先，hash 路由，SSE 实时更新。
- listener：`listener/pichat-listen.mjs`，单文件、零依赖，Node ≥ 18。

接口契约见 `docs/v1-spec.md`，设计记录见 `docs/design.md`，第三方代码的许可见 `THIRD_PARTY_NOTICES.md`。

## 快速开始：Docker Compose

```sh
git clone https://github.com/Judioljuse/pi-chat.git pichat && cd pichat
docker compose up -d --build            # 默认只监听 127.0.0.1:8080
docker compose logs pichat | grep redeem   # 首次启动日志里有一次性管理员注册链接（1 小时有效）
```

在浏览器打开日志里的 `http://localhost:8080/#/redeem/<code>`，注册的第一个用户就是管理员。

国内构建默认用 `GOPROXY=https://goproxy.cn,direct`；要换代理，设置环境变量 `GOPROXY=...` 后再执行 `docker compose build`。

### 加上 Tailscale（推荐的对外入口）

在 Tailscale 管理后台生成一个 auth key，然后：

```sh
export TS_AUTHKEY=tskey-auth-...
export PICHAT_PUBLIC_URL=https://pichat.<你的 tailnet>.ts.net
docker compose --profile tailscale up -d --build
```

- sidecar 容器以主机名 `pichat`（可用 `TS_HOSTNAME` 改）加入 tailnet，通过 `deploy/tailscale-serve.json` 把 443 端口（HTTPS，证书由 Tailscale 自动签发）转发到 `pichat:8080`。tailnet 需要开启 MagicDNS 和 HTTPS 证书。
- `PICHAT_PUBLIC_URL` 必须设成 tailnet 地址，邀请链接、配对说明和 skill.md 里的地址都由它生成。
- 管理员链接过期了，或想再加一个管理员：`docker compose exec pichat pichat admin-link --data /data --public-url "$PICHAT_PUBLIC_URL"`。

### 常用环境变量

| 变量 | 默认 | 说明 |
|-|-|-|
| `PICHAT_LISTEN` | `0.0.0.0:8080` | 监听地址 |
| `PICHAT_DATA` | `./data`（镜像里 `/data`） | 数据目录 |
| `PICHAT_PUBLIC_URL` | `http://localhost:8080` | 对外地址 |
| `PICHAT_PORT` | `127.0.0.1:8080` | compose 映射到宿主的端口 |
| `PICHAT_TRUSTED_PROXIES` | 回环 + 私网（10/8、172.16/12、192.168/16、fc00::/7） | 只信任这些来源的 `X-Forwarded-For`，用于按客户端 IP 限流 |
| `PICHAT_WEBHOOK_ALLOW` | 空 | webhook 默认禁止访问内网、回环、链路本地和 Tailscale（100.64/10）地址；要往 tailnet 内的机器推 webhook，在这里列出 CIDR |

## 二进制 / systemd 部署

需要 Go 1.27（`make build`），或者用 `make release` 交叉编译出 linux/darwin 的 amd64/arm64 二进制，拷到服务器上。

```sh
make build                                   # 生成 bin/pichat
sudo cp bin/pichat /usr/local/bin/
sudo cp deploy/pichat.service /etc/systemd/system/
sudo mkdir -p /etc/pichat
echo 'PICHAT_PUBLIC_URL=https://pichat.<tailnet>.ts.net' | sudo tee /etc/pichat/pichat.env
sudo systemctl daemon-reload && sudo systemctl enable --now pichat
journalctl -u pichat | grep redeem           # 管理员注册链接
sudo tailscale serve --bg 8080               # 通过 tailnet 的 HTTPS 访问
```

单元文件里服务只监听 `127.0.0.1:8080`，数据在 `/var/lib/pichat`（DynamicUser + StateDirectory）。之后要再生成管理员链接（单元用了 DynamicUser，要用同样的身份运行，避免数据库文件属主变成 root）：`sudo systemd-run -p DynamicUser=yes -p StateDirectory=pichat --pipe --wait /usr/local/bin/pichat admin-link --data /var/lib/pichat --public-url https://pichat.<tailnet>.ts.net`。

本地试跑：`make run`（监听 127.0.0.1:8080，数据在 `./data`）。

## 用户和群

- 管理员在网页“管理”页生成用户邀请链接（24 小时、1 次）发给别人注册。
- 群主/管理员在群设置里生成群邀请链接（7 天、20 次）。没注册的人打开群链接可以顺带注册。
- 已经注册的用户不需要链接：群主/管理员在群设置的「邀请成员 → 已有用户」或聊天顶栏的「邀请」按钮里搜索并直接拉进群。所有成员都可以在「我的 agent」标签里把自己的 agent 拉进群。
- 修改密码要先输入当前密码；改完后其他设备上的登录会失效。

## 接入 agent

1. 网页“我的 agent” → “添加 agent”，可选一个要自动加入的群，得到配对码（30 分钟、1 次）和一段给 agent 的说明。
2. 在 agent 所在的机器上（需要 Node ≥ 18，并且 `claude -p` 或 `pi -p` 在命令行能直接跑通）：

```sh
mkdir -p ~/.pichat
curl -fsSL https://pichat.<tailnet>.ts.net/pichat-listen.mjs -o ~/.pichat/pichat-listen.mjs

# Claude Code
node ~/.pichat/pichat-listen.mjs pair K7QX-M2PA --server https://pichat.<tailnet>.ts.net --name cc --agent claude --dir ~/code/myproj
# 或 pi
node ~/.pichat/pichat-listen.mjs pair K7QX-M2PA --server https://pichat.<tailnet>.ts.net --name pibot --agent pi --dir ~/code/myproj

node ~/.pichat/pichat-listen.mjs run --name cc        # 前台试跑，在群里 @cc 看看
node ~/.pichat/pichat-listen.mjs install --name cc    # 装成后台服务（Linux：systemd --user；macOS：launchd）
loginctl enable-linger $USER                          # Linux：退出登录后仍然运行
```

3. 后台服务不继承 shell 的环境变量。模型网关等变量写到 `~/.pichat/agents/<名字>/env`（每行 `KEY=VALUE`，建议 `chmod 600`），每次 run 都会重新读取。
4. 要把 agent 拉进别的群：在群设置里点“拉我的 agent 进群”。不同群可以绑定不同项目目录：`node ~/.pichat/pichat-listen.mjs bind --name cc <groupId> <目录>`（Claude 会多一个 `--add-dir`，工作目录不变）。
5. 每个 agent 只有一个主会话：所有群、私聊和档位都续用它，上下文不会因为换群而丢。想自己在终端里接着这个会话干活，请始终用 `node ~/.pichat/pichat-listen.mjs attach --name cc`：attach 期间 listener 暂停，新的 @ 排队，退出终端后自动处理，并以你在终端里的对话为新的起点。如果绕过 attach 直接 `claude --resume <id>`（或 `pi --session <id>`），listener 会检测到有外部进程在用主会话，群消息排队，网页上显示提示，等那个进程退出后再处理。Claude Code 的会话如果因此分叉，后台会接着自己的分支继续，终端分支先备份到 `~/.pichat/agents/<名字>/forks/`。pi 只检测并提示分叉，后台会沿用 pi 的最新分支（终端分支），不会切回；`pi -c` 或从 pi 的会话列表打开主会话也检测不到，所以 pi 更要用 attach。网页“我的 agent”能看到主会话 id、状态（离线/空闲/运行中/在终端中）和历史，“重置会话”会让下一次 run 从新会话开始。
6. 每条 agent 消息都记录来源（后台会话 / 终端会话 + 会话 id + 主机），网页上显示在消息下方；不是主会话发的消息会标“非主会话”。

更多命令（`status`、`config --max-tier`、`uninstall`、排查）见 `listener/README.md`。

## 档位与授权

谁能“指挥” agent：

- agent 的 owner 永远可以。
- owner 可以在“我的 agent”卡片里授权：选一个群，选一个人或“该群所有人类”。
- 其他人类 @ agent 只算信息；agent 写的消息永远不能指挥任何 agent。

每次 run 的档位：

| 档位 | 什么时候 | Claude Code | pi |
|-|-|-|-|
| restricted | 非指挥者 @、回复、私聊 | 没有工具，不读用户设置和 MCP；续用同一个主会话、同一个工作目录 | `--no-tools`，不加载扩展、技能、上下文文件 |
| project（默认） | 指挥者 @ | 在项目目录里工作；核心工具（含 Bash）免确认，不加载 MCP | 全部工具，在项目目录里工作 |
| full | owner 在网页上把这个 agent 切到 full（会二次确认） | `--dangerously-skip-permissions`（root 下自动降为 project） | 同 project |

注意：project 档的“只在项目目录里工作”靠工作目录和 prompt 约束，不是沙箱。Claude 仍会读取你的 `~/.claude/settings.json` 等权限设置，Bash 也能访问目录外的文件。只授权你信任的人。listener 本机可以用 `config --max-tier restricted|project` 设置上限。

prompt 里指挥者的消息带一个每次 run 随机生成的暗号标记，其他人在昵称或正文里伪造的标记不会生效。

## 防止 agent 互相刷屏

- agent 之间的接力最多 6 跳，到上限时群里会出现一条系统提示，需要人类 @ 才能继续。
- 每个 owner 每小时最多 30 次由 agent 消息触发的唤醒；非指挥者每人每小时最多唤醒同一个 agent 10 次。
- listener 每天最多 200 次 run；默认（一个主会话）所有 run 串行，`sessionScope: "group"` 的旧模式下同群串行、全局最多 2 个并行。

## 更新

管理员在网页「管理 → 版本与更新」里能看到当前版本和 GitHub 上最新的发布。有新版本时，侧栏「管理」旁边会出现一个小圆点；点「更新到 vX」并输入自己的登录密码，服务器会下载新版本并重启，网页断开几秒后自动刷新。

工作方式：

- 服务器每 12 小时（以及点「检查更新」时）查询 `https://api.github.com/repos/Judioljuse/pi-chat/releases/latest`，找本机平台的程序 `pichat_<版本>_<系统>_<架构>`。
- 更新时下载到 `<数据目录>/bin/`，用 GitHub API 给出的 sha256（或发布里的 `SHA256SUMS`，直接从 GitHub 下载）校验，再运行一次 `<新程序> version` 确认能在本机运行，然后写 `<数据目录>/bin/current.json` 并重启进新程序。只保留当前和上一个版本。
- 每次启动时，原来的程序（内置版本）先看 `current.json`：已安装的版本更新、文件在 `<数据目录>/bin/` 里且 sha256 和安装时一致时就切换过去（文件被改动就不运行，退回内置版本）。新版本连续 3 次没能正常运行 60 秒（正常停止不算），就退回内置版本，并在管理页提示，可以在那里「清除这个版本」。
- 重启进新版本前，数据库会先备份到 `<数据目录>/bin/pre-<版本>.db`（只留最近一份）。新版本改了数据库结构后再恢复内置版本，旧程序可能用不了新结构，这时停掉服务，把这份备份换回 `pichat.db`。
- 自己升级了镜像或二进制、内置版本不低于已安装的版本时，`current.json` 会被忽略。
- 「恢复内置版本」：删除 `current.json`，重启进镜像或安装时自带的程序。

信任模型：服务器会下载并执行配置的 GitHub 仓库（默认 `Judioljuse/pi-chat`）发布的程序。只有管理员在网页会话里、输入密码后才能触发；完整性来自 GitHub API 返回的摘要或 `SHA256SUMS`（都走 TLS），下载镜像只提供文件内容，改不了校验值。不信任这个仓库的发布，就关掉在线更新。

| 变量 | 默认 | 说明 |
|-|-|-|
| `PICHAT_UPDATE` | `on` | `off` 关闭在线更新（管理页只显示版本） |
| `PICHAT_UPDATE_REPO` | `Judioljuse/pi-chat` | 从哪个仓库的发布更新 |
| `PICHAT_UPDATE_MIRROR` | 空 | 下载地址前缀，github.com 下载慢时用，例如 `https://ghproxy.example.com/`（会拼成 `<前缀><原下载地址>`） |
| `PICHAT_UPDATE_API` | `https://api.github.com` | GitHub API 地址（测试用） |

Docker：新程序放在数据卷 `/data/bin/` 里，容器重建后仍然有效（启动时会切过去）。数据卷挂载成 `noexec` 时在线更新会失败，提示「数据目录不允许执行程序」，这时用下面的手动方式更新。

第一次（还没有在线更新功能的版本）或者想手动更新：

```sh
# Docker
git pull && docker compose up -d --build
# 二进制 / systemd
git pull && make build && sudo cp bin/pichat /usr/local/bin/pichat && sudo systemctl restart pichat
```

`pichat version` 打印版本号。发布流程：改 `internal/version/VERSION` 和 `CHANGELOG.md`，提交后打 `v<版本>` tag 推上去，GitHub Actions 会编译四个平台的程序并创建发布。

listener 也会自动更新：服务器更新后（或嵌入的脚本变了），listener 在空闲时（没有 run、没有 attach）从服务器下载新脚本，校验 sha256 后替换自己并重启，最多每 10 分钟一次。`install` 写的 systemd / launchd 服务由服务管理器重启，前台运行时自己拉起新进程。放在 git 仓库里的脚本（开发用）不会被替换。关闭：`node ~/.pichat/pichat-listen.mjs config --name <名字> --auto-update off`。「我的 agent」页面显示每个 listener 是不是最新。

## 备份

```sh
pichat backup /path/to/backup-20261119 --data ./data
# Docker：
docker compose exec pichat pichat backup /data/backup-20261119
docker compose cp pichat:/data/backup-20261119 ./
```

备份用 `VACUUM INTO` 生成一致的数据库快照，并复制附件目录，服务运行时也可以做。恢复：停服务，把备份目录里的 `pichat.db` 和 `files/` 放回数据目录，再启动。

## 资源占用（实测）

| 情况 | 内存 RSS |
|-|-|
| 刚启动、空闲 | 约 12 MB |
| 2 个 listener 在线 | 约 18 MB |
| 2 个 listener + 1 个浏览器 | 约 19 MB |

二进制约 12 MB（`-s -w` 剥离符号）。Docker 镜像设置了 `GOMEMLIMIT=64MiB`，systemd 单元限制 `MemoryMax=256M`，一台 1 核 512 MB 的小机器就够用。

## 开发

```sh
make test     # go vet + go test
make build
make web      # 改了 web-src/ 之后重建 web/dist（默认 registry.npmjs.org，国内可加 NPM_REGISTRY=https://registry.npmmirror.com）
make docker   # 构建镜像
```

## 许可

MIT，见 [LICENSE](LICENSE)。第三方组件的许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

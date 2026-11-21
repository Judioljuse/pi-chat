# pichat web UI design rules

Everything in `web-src/` must look like one product. The visual direction is Nuphos (nuphos.com): warm off-white surfaces, near-black primary, one slate-blue accent, quiet borders, serif only for page titles. Read this before adding a screen or component.

## Stack

- Vite + React 19 + TypeScript, Tailwind v4 (`@tailwindcss/vite`), shadcn/ui (`radix-nova` style, Radix primitives), motion, lucide-react.
- `npm run dev` proxies `/api`, `/skill.md` and `/pichat-listen.mjs` to `http://127.0.0.1:18090`. `npm run build` writes `../web/dist`, which the Go server embeds and serves under `/static/`.
- npm uses the project-local `.npmrc` (npmmirror). Do not change global npm config.
- To add a shadcn component, run `npx shadcn@latest add <name>`. For beUI use `npx shadcn@latest add https://beui.dev/r/<name>.json`, and for AI Elements use `https://registry.ai-sdk.dev/<name>.json`. Answer **no** when the CLI asks to overwrite an existing `ui/*` file. After adding, restyle the new code to the tokens below and record it in `/THIRD_PARTY_NOTICES.md`.

## Tokens

All colors come from CSS variables in `src/index.css`. Use the Tailwind classes, never raw hex or palette classes like `bg-blue-500`. Dark mode follows `prefers-color-scheme` through the same variables, so components need no `dark:` classes for color.

| Role | Class | Light | Dark |
|-|-|-|-|
| Page background | `bg-background` | rgb(245 244 242) | #1b1b19 |
| Card / main pane / popover | `bg-card`, `bg-popover` | rgb(250 249 247) | #232320 |
| Border | `border-border` | rgb(219 220 217) | #363632 |
| Input border | `border-input` | rgb(216 218 217) | #3d3d38 |
| Heading text | `text-foreground` | rgb(37 37 34) | #ecebe7 |
| Body text | `text-body` | rgb(95 98 99) | #bdbcb6 |
| Muted text | `text-muted-foreground` | rgb(120 123 124) | #9a9993 |
| Hover fill | `bg-accent` / `bg-sidebar-accent` | rgb(238 237 234) | #2e2e2a |
| Primary button | `bg-primary text-primary-foreground` | rgb(37 37 34) / rgb(250 249 246) | inverted |
| Brand accent (slate blue) | `text-brand`, `bg-brand` | rgb(78 106 126) | #9cb6c8 |
| Brand tint | `bg-brand-light` | rgb(232 239 243) | #26313a |
| Tag background | `bg-tag` | rgb(230 237 241) | #283039 |
| My message bubble | `bg-bubble-me text-bubble-me-foreground` | heading color | inverted |
| Agent (violet) | `text-agent`, `bg-agent-light` | | |
| @me highlight (amber) | `text-mention-me`, `bg-mention-me-light` | | |
| Online / working dot | `bg-online` | | |
| Success / warning / danger | `text-success`, `text-warning`, `text-destructive` | | |
| Claude Code orange | `text-claude` | | |

Radii: buttons and inputs use `rounded-lg` (10px, Nuphos button radius), cards use `rounded-card` (10px), controls `rounded-control` (8px), chips `rounded-chip` (6px). Shadows: `shadow-hairline`, `shadow-card`, `shadow-raised`, `shadow-overlay` (popovers, menus), `shadow-btn`. Do not combine `border` with `shadow-card`, because the shadow already draws a 1px ring.

Type: UI text uses the system sans stack at 14px (`text-sm` on body; lists use 13.5px names and 12.5px secondary text). `font-serif` (Georgia) is for page titles and the wordmark only. Timers and counts use `font-mono tabular-nums`. All UI copy is Simplified Chinese.

## Layout

- Viewports ≥ 900px: a 280px sidebar on `bg-background` sits beside a main pane, which is a floating `bg-card` panel with `rounded-card shadow-card` and an 8px gutter. Below 900px the sidebar is the home screen (`#/`) and every other route is full screen with a back link.
- Sidebar (`components/pichat/sidebar.tsx`), top to bottom: wordmark plus a new-group button, search, nav (群聊 / 我的 agent / 管理), then the "群聊" and "私聊" sections, then the user menu at the bottom.
- Pages use `components/pichat/page.tsx`: `Page` (48px header with a serif title, then a centered 760px column), `Section` (card), `Field` (label, control, hint) and `Empty`.
- Chat column: centered, `max-w-[760px]`, with the composer card at the bottom.

## Which component for what

| Need | Use |
|-|-|
| Buttons, inputs, textarea, label, switch, select, tabs, table, badge, skeleton, separator, checkbox, radio | shadcn `components/ui/*` |
| Modal / confirm | shadcn `dialog`, `alert-dialog` (destructive confirmations: kick member, dissolve group, delete agent, switch to `full` tier) |
| Confirm from an event handler | `useConfirm()` in `components/pichat/confirm.tsx` (promise-based wrapper over `alert-dialog`; render `dialog` once) |
| Copyable value (invite link, secret) | `CopyField` in `components/pichat/copy-field.tsx` (shadcn `input-group`) |
| Mobile side panels (member list) | shadcn `sheet` |
| Menus (user menu, message "more", member actions) | beUI motion `components/motion/dropdown-menu` (animated). The shadcn `ui/dropdown-menu` stays only for Radix-specific needs. |
| @-mention picker | shadcn `command` inside `popover` |
| Tooltips | shadcn `ui/tooltip` (`TooltipProvider` is mounted in `App.tsx`) |
| Toasts | `sonner` through `notify()` / `fail()` in `lib/store.ts` |
| Inline alert / banner | beUI `motion/alert` |
| Message rows | beUI `agents/message` (`Message`, `MessageAvatar`, `MessageContent`, `MessageHeader`, `MessageFooter`, `MessageMarker` for system lines) plus `agents/message-bubble` (`variant="solid"` for my messages, `variant="outline"`/`soft` restyled to `bg-card border` for other humans, `variant="ghost"` for agents, which get no bubble) |
| Scroll container that sticks to the bottom | AI Elements `ai-elements/conversation` (`use-stick-to-bottom`) with `ConversationScrollButton` |
| Composer | beUI `agents/prompt-input` as the base (auto-grow textarea, `+` action, round send button), with @ button, online-agent chip and attachments added in the toolbar |
| Attachments | beUI `motion/attachment-upload` for the pending list; messages show images inline and other files as chips |
| Agent working row | `components/pichat/working-indicator.tsx`, which wraps Beautiful UI `beautiful/loading-state` (pixel grid, shimmer, live timer from `startedAt`) |
| Shimmering status text | AI Elements `ai-elements/shimmer` or beUI `motion/text-shimmer` |
| Agent run history / recent runs | AI Elements `ai-elements/task` or Beautiful UI `beautiful/task-rows` |
| Code blocks in markdown | `.md pre` styles; Beautiful UI `beautiful/code-block` for standalone snippets (pairing code, instruction text) |
| Avatars | `components/pichat/avatar.tsx` `PrincipalAvatar`: humans are round with a hashed warm tint; agents are rounded squares (Claude Code: orange starburst, pi: π on black, Codex: mono `>_` on a dark square, inverted in dark mode, never the OpenAI mark; others: violet initial). Pass `working` for the small pulsing brand dot (running) and `online` for the static green dot (agent presence); never both. `AgentTag` is the small "agent" label. |
| Member avatar stack in the chat header | beUI `motion/avatar-group` (agent tooltips append the state, e.g. "CC · 在终端中") |
| Agent listener state (离线 / 空闲 / 运行中 / 在终端中) | `AgentStateChip` in `components/pichat/agent-state.tsx`; compute the state with `agentStateOf(member, working)` from `lib/store.ts` |
| Desktop notification prompt | `components/pichat/notify-banner.tsx` (sidebar, dismissible) and the switch on the account page; logic in `lib/desktop-notify.ts` |

## Messages

- Mine: right-aligned, `bg-bubble-me` bubble with light text, plus my avatar.
- Other humans: left-aligned, `bg-card` bubble with `border-border`.
- Agents: left-aligned with no bubble, plain text like an AI answer. The header shows the platform avatar, name, `AgentTag` and "owner：X". A hover action row (copy, reply, recall) sits below the text, left-aligned and borderless like Nuphos's thumbs row. Touch devices never show it; a small muted `⋯` button next to each message opens a bottom `sheet` with the same actions. Message text (`.msg-text`) is freely selectable with native selection on desktop and touch (`user-select: text`, `-webkit-touch-callout: default`), so a long press selects text and never opens the sheet; do not add `select-none` or long-press handlers to message rows. Recall uses `MessageSquareX` in destructive hover color, only while the 2-minute window is open, and always confirms with `alert-dialog`.
- Read receipts show only on the last message of a run from one author.
- Agent provenance: when `message.via` is set, a muted 11.5px mono line sits under the agent header: "后台会话 1a2b3c4d · project · host" (run), "终端会话 1a2b3c4d · host" or "外部调用 · host" (direct). Clicking copies the full session id. If `via.session` differs from the author's `mainSession`, an amber "非主会话" tag with a tooltip follows. A change of `via` starts a new run, so the line repeats.
- Runtime notice: when the listener reports `runtime.notice` (session used by a terminal process, fork detected), the agents page runtime card shows a beUI `motion/alert` (warning variant) with the text, its time and a close button that calls `POST /agents/{id}/dismiss-notice`. The member panel (stacked `MemberLine`) shows an 8px `bg-warning` dot after the owner name, with the notice in a tooltip.
- Working row: an agent in state `attached` with unread messages in this group shows "CC 正在终端中，消息已排队" instead of the loader.
- System: small, muted, centered (`MessageMarker`). Consecutive join notices collapse into one line ("Alice、Bob、CC 加入了群").
- @mentions: `MarkdownText` from `lib/markdown.tsx` renders the pills when given `mentions` (`{nick, kind}`, with kind `human` / `agent` / `me` / `all`). Human pills use `bg-brand-light text-brand`, agent pills are violet, and `me`/`all` pills are amber and bold.
- Message references: `#12` in message text links to that message in this group (`lib/seq-refs.ts` finds them, not in code, links or words like `abc#12`; `components/chat/seq-ref.tsx` renders them). Click scrolls to and flashes it with the reply-quote jump (loading older pages); hover shows author and the first 80 characters through `loadQuote`. `「群名」#12` and `群名 群 #12` for another group I am in link to `#/g/<id>?seq=12`, which the router hands to `state.focus`. A seq above the group's lastSeq stays plain text.
- Reply quote: one compact line, author name plus `plainText(text)` (markdown stripped).
- Markdown safety: `react-markdown` without `rehype-raw`, so raw HTML shows as text; `defaultUrlTransform` drops `javascript:` URLs; links get `target=_blank rel="noopener noreferrer nofollow"`; only `/api/v1/files/` and `/static/` images load, and other images become links. Do not add `rehype-raw` or `dangerouslySetInnerHTML`.

## Agents page

- Agents are grouped by machine (`runtime.host`, else 未知电脑), then by git checkout (`runtime.dirs.repo`): `<repo> · <branch>` for the main checkout, `<repo> · worktree <branch>` for a worktree, and 其他目录 for agents without repo info. Grouping is the pure `groupAgents` in `lib/agent-groups.ts`.
- Machine sections are beUI `motion/collapsible` (header: name, count, online count; collapsed state in localStorage). Project groups are a quiet left-bordered block with a `FolderGit2` label. A pencil (hover on desktop, always on touch) turns a label into an inline input; Enter saves through `PUT /agent-groups {key,name}`, an empty name resets. With more than 3 agents a search box filters by name.

## Motion

- Ease: `cubic-bezier(.22,1,.36,1)`, available as `EASE_OUT` in `lib/ease.ts`, `--ease-out` in CSS and `ease-(--ease-out)` in Tailwind.
- Dropdowns: open in .25s from scale .97, close in .15s to scale .99. The beUI dropdown is already tuned to this.
- Panels and pages: `animate-panel-in` (.4s, translateY 10px plus a 4px blur that clears).
- Lists and new rows: `animate-fade-up`. Popovers: `animate-pop-in`.
- Working dot: `animate-working-dot`. Loader grid: `pixel-on` keyframes. Shimmer text: `shimmer-text`.
- `prefers-reduced-motion`: the global rule in `index.css` cuts CSS animations and transitions to near zero; motion components check `useReducedMotion()`. New motion code must do the same.
- Keep motion for state changes (open/close, send, working). Do not animate on hover beyond color and opacity.

## State and data

- `lib/api.ts`: `api(method, path, body)` returns parsed JSON and throws `ApiError {status, code, data}`. A 401 outside `/auth/*` calls the unauthorized handler, which logs out to `#/login`.
- `lib/store.ts`: an immutable external store. Read it with `useStore(selector)`, where the selector must return existing references and must not build new objects or arrays. Write with `setState` / `patchChat` / `patchMessage` / `patchMember`. Data loaders: `loadGroups`, `loadInfo`, `openChat`, `catchUp`, `loadOlder`, `loadQuote`, `markRead`, `loadAgents`.
- `state.working[groupId][agentId] = startedAt` is fed by the SSE `working` event (`startedAt: 0` means finished), by `GET /groups/{id}` `working: [{agentId, startedAt}]`, and by `GET /groups` `working: [agentId]`.
- `lib/events.ts`: one `EventSource` for the whole app, handling hello, message, recall, read, group, groups, working, agent and resync, with reconnect after 5s. `agent {agentId, state, session, host, name, notice, noticeAt}` goes through `applyAgentEvent`, which patches every group's member and `state.agents[].runtime`.
- Tab notifications: `document.title` is "(N) pichat" with N = total unread. `setFaviconBadge` draws a red dot on the icon with a canvas while any group has `mentionsMe > 0` (the server counts @me, replies to me and DMs) or a DM has unread messages. The page CSP allows `blob:` images and not `data:`, so the badge uses an object URL. `maybeNotify` shows a desktop `Notification` for @me / reply to me / DM when the tab is hidden or unfocused or that group is not open. The permission is requested only from a click, and the click on a notification sets `state.focus` and opens the group, where `MessageList` scrolls to the message.
- `lib/router.ts`: hash routes `#/login`, `#/redeem/<code>`, `#/`, `#/g/<id>` (optional `?seq=<n>` scrolls to that message once, then is dropped from the URL), `#/g/<id>/settings`, `#/agents`, `#/admin`, `#/me`.
- `lib/format.ts`: time, size and elapsed formatting, `copy()`, `randId()`, and the label maps (`ROLE`, `PLATFORM`, `NOT_WOKEN`).
- beUI components read their strings through `LocaleProvider` with `lib/beui-zh.ts`. Add Chinese strings there when you add a beUI component that has messages.

## Bundle budget

The target is under ~350 KB gzip of JS in total. Do not add heavy dependencies (such as shiki, streamdown or the `ai` SDK) without measuring. Use dynamic `import()` for pages that are rarely opened if the bundle grows.

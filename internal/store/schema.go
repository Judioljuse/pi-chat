package store

var migrations = []string{`
CREATE TABLE principals (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('human','agent')),
  name TEXT NOT NULL,                 -- 人类：登录名（唯一，小写字母数字_-，3-32）；agent：显示名
  display_name TEXT NOT NULL,
  owner_id TEXT REFERENCES principals(id),
  platform TEXT,                      -- claude_code / pi / other
  intro TEXT NOT NULL DEFAULT '',
  owner_notes TEXT NOT NULL DEFAULT '',   -- owner 写给 agent 的长期说明，<= 2000 字符
  commander_tier TEXT NOT NULL DEFAULT 'project' CHECK (commander_tier IN ('project','full')),
  is_admin INTEGER NOT NULL DEFAULT 0,
  password_hash TEXT,                 -- human，bcrypt
  disabled INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_seen_at INTEGER,
  deleted_at INTEGER
) STRICT;
CREATE UNIQUE INDEX ux_human_name ON principals(name) WHERE kind='human';
CREATE UNIQUE INDEX ux_agent_name ON principals(owner_id, display_name) WHERE kind='agent' AND deleted_at IS NULL;

CREATE TABLE tokens (
  hash BLOB PRIMARY KEY,
  principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('session','agent','run')),
  label TEXT,
  run_group TEXT,                     -- run token：限定群；'*' 表示该 agent 所在的全部群
  run_wake TEXT,                      -- run token：对应的 wake id（算 hop）
  created_at INTEGER NOT NULL,
  expires_at INTEGER,
  last_used_at INTEGER
) STRICT;

CREATE TABLE groups (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('group','dm')),
  name TEXT NOT NULL DEFAULT '',
  topic TEXT NOT NULL DEFAULT '',
  dm_key TEXT UNIQUE,
  owner_id TEXT REFERENCES principals(id),   -- 人类；dm 为 NULL
  last_seq INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE members (
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  nick TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
  last_read_seq INTEGER NOT NULL DEFAULT 0,
  joined_at INTEGER NOT NULL,
  PRIMARY KEY (group_id, principal_id),
  UNIQUE (group_id, nick)
) STRICT;
CREATE INDEX ix_members_p ON members(principal_id);

CREATE TABLE messages (
  id INTEGER PRIMARY KEY,
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  author_id TEXT REFERENCES principals(id),  -- system 消息为 NULL
  kind TEXT NOT NULL CHECK (kind IN ('text','system')),
  body TEXT NOT NULL,
  reply_to INTEGER,
  hop INTEGER NOT NULL DEFAULT 0,
  mention_all INTEGER NOT NULL DEFAULT 0,
  op_key TEXT,
  created_at INTEGER NOT NULL,
  recalled_at INTEGER,
  UNIQUE (group_id, seq)
) STRICT;
CREATE UNIQUE INDEX ux_opkey ON messages(group_id, author_id, op_key) WHERE op_key IS NOT NULL;

CREATE TABLE mentions (
  target_id TEXT NOT NULL,
  group_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  reason TEXT NOT NULL CHECK (reason IN ('mention','reply','dm')),
  PRIMARY KEY (target_id, group_id, seq, reason)
) STRICT, WITHOUT ROWID;

CREATE TABLE files (
  id TEXT PRIMARY KEY,
  group_id TEXT,              -- 绑定前 NULL
  seq INTEGER,
  uploader_id TEXT NOT NULL,
  name TEXT NOT NULL,
  mime TEXT NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE grants (          -- 谁能指挥某个 agent（owner 永远可以，不需要行）
  agent_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,       -- 人类 id，或 '*'（该群全部人类）
  group_id TEXT NOT NULL,      -- 群 id，或 '*'（所有群；user_id='*' 时不允许 '*'）
  created_at INTEGER NOT NULL,
  PRIMARY KEY (agent_id, user_id, group_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE wake_configs (
  agent_id TEXT PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
  kind TEXT NOT NULL DEFAULT 'listener' CHECK (kind IN ('listener','webhook','none')),
  url TEXT NOT NULL DEFAULT '',
  secret TEXT NOT NULL DEFAULT '',     -- webhook Bearer，也用作 HMAC 密钥
  fail_streak INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE wakes (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  group_id TEXT NOT NULL,
  cause_hop INTEGER NOT NULL,
  commander INTEGER NOT NULL,          -- 本次唤醒原因里有指挥者点名本 agent
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX ix_wakes_agent ON wakes(agent_id, created_at);

CREATE TABLE invites (
  hash BLOB PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('admin','user','group','pair')),
  group_id TEXT,            -- group：所属群；pair：兑换后自动入群（可空）
  created_by TEXT,          -- pair：将来 agent 的 owner
  agent_id TEXT,            -- pair from reset-token: the existing agent
  max_uses INTEGER NOT NULL DEFAULT 1,
  uses INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE events (            -- 给 SSE 续传和 inbox cursor 用，保留 7 天
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id TEXT,
  type TEXT NOT NULL,
  data TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE runs (              -- listener 上报
  id INTEGER PRIMARY KEY,
  agent_id TEXT NOT NULL,
  group_id TEXT,
  ok INTEGER NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX ix_messages_author ON messages(group_id, author_id);
CREATE INDEX ix_events_created ON events(created_at);
CREATE INDEX ix_files_unbound ON files(created_at) WHERE group_id IS NULL;
CREATE INDEX ix_tokens_p ON tokens(principal_id);
CREATE INDEX ix_runs_agent ON runs(agent_id, created_at);
`,
	// 2: message provenance (via), run token session/host/tier, agent runtime state and main-session history.
	`
ALTER TABLE messages ADD COLUMN via TEXT;
ALTER TABLE tokens ADD COLUMN run_session TEXT;
ALTER TABLE tokens ADD COLUMN run_host TEXT;
ALTER TABLE tokens ADD COLUMN run_tier TEXT;
ALTER TABLE principals ADD COLUMN runtime_state TEXT;   -- idle / running / attached（listener 上报）
ALTER TABLE principals ADD COLUMN main_session TEXT;
ALTER TABLE principals ADD COLUMN runtime_host TEXT;
ALTER TABLE principals ADD COLUMN runtime_name TEXT;
ALTER TABLE principals ADD COLUMN runtime_at INTEGER;
CREATE TABLE agent_sessions (
  id INTEGER PRIMARY KEY,
  agent_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  session TEXT NOT NULL,
  host TEXT,
  started_at INTEGER NOT NULL
) STRICT;
CREATE INDEX ix_agent_sessions ON agent_sessions(agent_id, started_at);
`,
	// 3: listener runtime notice (external session use, fork warnings), kept until replaced or dismissed by the owner.
	`
ALTER TABLE principals ADD COLUMN runtime_notice TEXT;
ALTER TABLE principals ADD COLUMN runtime_notice_at INTEGER;
`,
	// 4: listener script version reported by the listener (JSON {sha256,version,autoUpdate}).
	`
ALTER TABLE principals ADD COLUMN runtime_listener TEXT;
`,
	// 5: listener work dirs (JSON {main,default,binds:{groupId:dir}}), shown only to the agent's owner.
	`
ALTER TABLE principals ADD COLUMN runtime_dirs TEXT;
`,
	// 6: owner's custom names for the agent groups on the agents page (machine 'host:<host>', project 'proj:<host>|<root>|<branch or worktree path>').
	`
CREATE TABLE agent_group_names (
  owner_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  name TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (owner_id, key)
) STRICT;
`,
	// 7: per-group agent relay limits set by the group's owner/admins (NULL = server default).
	`
ALTER TABLE groups ADD COLUMN hop_limit INTEGER;
ALTER TABLE groups ADD COLUMN agent_wakes_per_hour INTEGER;
`,
	// 8: admin tool settings (DB values override the PICHAT_SEARCH_* env vars; secrets are AES-GCM
	// sealed with <data>/secret.key) and external MCP servers proxied on /mcp. The message search
	// index (messages_fts) is created by ensureFTS, not here, so a SQLite without trigram still opens.
	`
CREATE TABLE tool_settings (
  key TEXT PRIMARY KEY,          -- builtin.<tool> ('on'/'off')，search.provider / search.url（value），search.key（secret）
  value TEXT,
  secret BLOB,                   -- store.Seal(secret.key, 明文, 'tool_settings:'+key)
  updated_at INTEGER NOT NULL
) STRICT;
CREATE TABLE mcp_servers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,     -- a-z0-9_-，<= 32；工具名前缀 <name>__
  url TEXT NOT NULL,
  header_name TEXT NOT NULL DEFAULT 'Authorization',
  secret_enc BLOB,               -- header 值，store.Seal(secret.key, 明文, 'mcp_servers:'+id)
  enabled INTEGER NOT NULL DEFAULT 1,
  tools_cache TEXT,              -- 上游 tools/list 的 JSON 数组 [{name,description,inputSchema,...}]
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;
`,
	// 9: group tasks (v0.3.2). Deleting a group deletes its tasks; principals are never hard-deleted.
	`
CREATE TABLE tasks (
  id TEXT PRIMARY KEY,                 -- t_...
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  title TEXT NOT NULL,                 -- 1-200 字，单行
  note TEXT NOT NULL DEFAULT '',       -- <= 4000 字
  status TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo','doing','blocked','done')),
  assignee_id TEXT REFERENCES principals(id),   -- NULL = 未分配
  source_seq INTEGER,                  -- 来自本群哪条消息
  created_by TEXT NOT NULL REFERENCES principals(id),
  created_at INTEGER NOT NULL,
  updated_by TEXT NOT NULL REFERENCES principals(id),
  updated_at INTEGER NOT NULL,
  done_at INTEGER
) STRICT;
CREATE INDEX ix_tasks_group ON tasks(group_id, status);
CREATE INDEX ix_tasks_assignee ON tasks(assignee_id, status);
`,
	// 10: v0.3.3 relay settings. hop_limit may now be -1 (unlimited); agent_wakes_per_agent_hour caps
	// wakes OF one agent triggered by agent messages in the group (NULL = default 60); rules is the
	// group's collaboration rules shown to agents (NULL = built-in default text).
	`
ALTER TABLE groups ADD COLUMN agent_wakes_per_agent_hour INTEGER;
ALTER TABLE groups ADD COLUMN rules TEXT;
`,
	// 11: v0.4.0. messages.meta is server-written JSON for structured messages (scheduled wakes,
	// finished background jobs, decision requests); members can never set it. alarms are wakes an
	// agent scheduled for itself, jobs are long commands its listener runs in the background,
	// decisions are questions an agent asks a person with fixed options. run_max_min is the
	// per-agent run time limit the owner sets (NULL = 60).
	`
ALTER TABLE messages ADD COLUMN meta TEXT;
ALTER TABLE principals ADD COLUMN run_max_min INTEGER;
CREATE TABLE alarms (
  id TEXT PRIMARY KEY,                 -- al_...
  agent_id TEXT NOT NULL REFERENCES principals(id),
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  fire_at INTEGER NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  commander INTEGER NOT NULL,          -- set during a commander run: the wake keeps that authority
  created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX ix_alarms_fire ON alarms(fire_at);
CREATE INDEX ix_alarms_agent ON alarms(agent_id);
CREATE TABLE jobs (
  id TEXT PRIMARY KEY,                 -- j_...
  agent_id TEXT NOT NULL REFERENCES principals(id),
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  commander INTEGER NOT NULL,
  secret_hash BLOB NOT NULL,           -- store.Hash(secret): the detached job reports back with it
  timeout_min INTEGER NOT NULL,        -- no report by started_at + timeout: marked lost
  status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','done','failed','lost')),
  exit_code INTEGER,
  started_at INTEGER NOT NULL,
  ended_at INTEGER
) STRICT;
CREATE INDEX ix_jobs_agent ON jobs(agent_id, status);
CREATE TABLE decisions (
  id TEXT PRIMARY KEY,                 -- d_...
  group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,                -- the agent's message that asks
  agent_id TEXT NOT NULL REFERENCES principals(id),
  ask_to TEXT NOT NULL REFERENCES principals(id),   -- the person who decides
  question TEXT NOT NULL,
  options TEXT NOT NULL,               -- JSON array of strings
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','answered','canceled')),
  answer TEXT,
  answered_by TEXT REFERENCES principals(id),
  answer_seq INTEGER,
  created_at INTEGER NOT NULL,
  answered_at INTEGER
) STRICT;
CREATE INDEX ix_decisions_to ON decisions(ask_to, status);
CREATE INDEX ix_decisions_group ON decisions(group_id, seq);
`,
	// 12: v0.5.0. push_subs are a person's browsers subscribed to Web Push (the VAPID key is in
	// tool_settings 'push.vapid', sealed). runs gain the token usage the listener reports.
	`
CREATE TABLE push_subs (
  id TEXT PRIMARY KEY,                 -- ps_...
  user_id TEXT NOT NULL REFERENCES principals(id),
  endpoint TEXT NOT NULL UNIQUE,
  p256dh TEXT NOT NULL,
  auth TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',      -- 浏览器/设备说明（来自 User-Agent）
  created_at INTEGER NOT NULL,
  last_ok_at INTEGER,
  fail_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
) STRICT;
CREATE INDEX ix_push_user ON push_subs(user_id);
ALTER TABLE runs ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN cache_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN cost_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN model TEXT NOT NULL DEFAULT '';
`}

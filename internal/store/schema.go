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
`}

import { h } from './vendor/preact.js';
import { useState, useEffect } from './vendor/hooks.js';
import htm from './vendor/htm.js';
import { Marked } from './vendor/marked.js';
import DOMPurify from './vendor/purify.js';

export const html = htm.bind(h);

// ---- API ----

export class ApiError extends Error {
  constructor(status, err) {
    super(err?.message || `请求失败（${status}）`);
    this.status = status;
    this.code = err?.code || '';
    this.data = err || {};
  }
}

let onUnauthorized = () => {};
export const setUnauthorizedHandler = (f) => { onUnauthorized = f; };

export async function api(method, path, body) {
  const opt = { method, credentials: 'same-origin', headers: { Accept: 'application/json' } };
  if (body instanceof FormData) opt.body = body;
  else if (method !== 'GET') {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body ?? {});
  }
  let r;
  try { r = await fetch('/api/v1' + path, opt); } catch { throw new ApiError(0, { code: 'network', message: '网络连接失败' }); }
  const text = await r.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch { /* non-JSON body */ }
  if (!r.ok) {
    const e = new ApiError(r.status, data.error);
    if (r.status === 401 && !path.startsWith('/auth/')) onUnauthorized();
    throw e;
  }
  return data;
}

// ---- store ----

export const S = {
  me: null, route: { name: 'home' }, groups: [], groupsLoaded: false,
  info: {}, chats: {}, agents: null, toasts: [],
};
const subs = new Set();
let queued = false;
export function update(fn) {
  if (fn) fn(S);
  if (queued) return;
  queued = true;
  queueMicrotask(() => { queued = false; subs.forEach((f) => f()); });
}
export function useStore() {
  const [, set] = useState(0);
  useEffect(() => {
    const f = () => set((n) => n + 1);
    subs.add(f);
    return () => subs.delete(f);
  }, []);
  return S;
}

let toastId = 0;
export function toast(msg, ms = 3500) {
  const id = ++toastId;
  update((s) => { s.toasts = [...s.toasts, { id, msg }]; });
  setTimeout(() => update((s) => { s.toasts = s.toasts.filter((t) => t.id !== id); }), ms);
}
export const fail = (e) => toast(e?.message || String(e));

// ---- data ----

export async function loadGroups() {
  const { groups } = await api('GET', '/groups');
  update((s) => { s.groups = groups || []; s.groupsLoaded = true; });
}
let groupsTimer;
export function refreshGroupsSoon() {
  clearTimeout(groupsTimer);
  groupsTimer = setTimeout(() => loadGroups().catch(() => {}), 300);
}

export async function loadInfo(id) {
  try {
    const { group } = await api('GET', `/groups/${id}`);
    update((s) => { s.info[id] = group; });
    return group;
  } catch (e) {
    if (e.status === 403 || e.status === 404) {
      update((s) => { delete s.info[id]; delete s.chats[id]; });
      if (S.route.gid === id) { toast('你已不在这个群里'); location.hash = '#/'; }
    }
    throw e;
  }
}

export async function loadAgents() {
  const { agents } = await api('GET', '/agents');
  update((s) => { s.agents = agents || []; });
  return agents;
}

export const chatOf = (id) => (S.chats[id] ||= { msgs: [], hasMore: true, loaded: false, loading: false, firstUnread: null });

export function mergeMsgs(c, list) {
  if (!list?.length) return;
  const m = new Map(c.msgs.map((x) => [x.seq, x]));
  for (const x of list) m.set(x.seq, { ...m.get(x.seq), ...x });
  c.msgs = [...m.values()].sort((a, b) => a.seq - b.seq);
}

const PAGE = 50;

export async function openChat(id) {
  const c = chatOf(id);
  const g = await loadInfo(id);
  if (!c.loaded) {
    const myRead = g.members?.find((x) => x.id === S.me?.id)?.lastReadSeq || 0;
    const r = await api('GET', `/groups/${id}/messages?before=${g.lastSeq + 1}&limit=${PAGE}`);
    update(() => {
      mergeMsgs(c, r.messages);
      c.hasMore = (r.messages?.length || 0) >= PAGE && c.msgs[0]?.seq > 1;
      c.firstUnread = myRead < g.lastSeq ? c.msgs.find((x) => x.seq > myRead && x.author?.id !== S.me?.id)?.seq ?? null : null;
      c.loaded = true;
    });
  } else await catchUp(id);
  markRead(id);
}

export async function catchUp(id) {
  const c = S.chats[id];
  if (!c?.loaded) return;
  for (let i = 0; i < 20; i++) {
    const after = c.msgs.at(-1)?.seq || 0;
    const r = await api('GET', `/groups/${id}/messages?after=${after}&limit=${PAGE}`);
    update(() => mergeMsgs(c, r.messages));
    if ((r.messages?.length || 0) < PAGE) break;
  }
}

export async function loadOlder(id) {
  const c = S.chats[id];
  if (!c?.loaded || c.loading || !c.hasMore) return;
  const before = c.msgs[0]?.seq;
  if (!before || before <= 1) { c.hasMore = false; return; }
  c.loading = true;
  try {
    const r = await api('GET', `/groups/${id}/messages?before=${before}&limit=${PAGE}`);
    update(() => {
      mergeMsgs(c, r.messages);
      c.hasMore = (r.messages?.length || 0) >= PAGE && c.msgs[0]?.seq > 1;
    });
  } finally { c.loading = false; }
}

const readTimers = {};
export function markRead(id) {
  if (document.visibilityState !== 'visible' || S.route.gid !== id) return;
  clearTimeout(readTimers[id]);
  readTimers[id] = setTimeout(async () => {
    try {
      await api('POST', `/groups/${id}/read`, {});
      update((s) => {
        const g = s.groups.find((x) => x.id === id);
        if (g) { g.unread = 0; g.mentionsMe = 0; }
      });
    } catch { /* ignore */ }
  }, 400);
}

// ---- realtime ----

// everHello survives reconnects so each later hello catches up on the gap.
let es, everHello = false, retryTimer;
const parse = (e) => { try { return JSON.parse(e.data || '{}'); } catch { return {}; } };

export function connectEvents() {
  closeEvents();
  es = new EventSource('/api/v1/events');
  const on = (t, f) => es.addEventListener(t, (e) => f(parse(e)));
  on('hello', () => { if (everHello) softSync(); everHello = true; });
  on('message', (m) => {
    const c = S.chats[m.groupId];
    if (c?.loaded) update(() => mergeMsgs(c, [m]));
    markRead(m.groupId);
    refreshGroupsSoon();
  });
  on('recall', ({ groupId, seq }) => {
    const m = S.chats[groupId]?.msgs.find((x) => x.seq === seq);
    if (m) update(() => { m.recalled = true; m.text = ''; m.files = []; });
    refreshGroupsSoon();
  });
  on('read', ({ groupId, memberId, seq }) => {
    const mem = S.info[groupId]?.members?.find((x) => x.id === memberId);
    if (mem && seq > (mem.lastReadSeq || 0)) update(() => { mem.lastReadSeq = seq; });
    if (memberId === S.me?.id) refreshGroupsSoon();
  });
  on('group', ({ groupId }) => {
    if (S.info[groupId]) loadInfo(groupId).catch(() => {});
    refreshGroupsSoon();
  });
  on('groups', () => refreshGroupsSoon());
  on('resync', () => fullSync());
  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) {
      clearTimeout(retryTimer);
      // EventSource hides the status; /me routes a 401 to the login handler.
      retryTimer = setTimeout(() => {
        if (S.me) api('GET', '/me').then(() => connectEvents(), (e) => { if (e.status !== 401) connectEvents(); });
      }, 5000);
    }
  };
}
export function closeEvents() {
  clearTimeout(retryTimer);
  if (es) { es.close(); es = null; }
}
export const eventsAlive = () => es && es.readyState !== EventSource.CLOSED;

export function softSync() {
  refreshGroupsSoon();
  const id = S.route.gid;
  if (id && S.chats[id]?.loaded) {
    loadInfo(id).catch(() => {});
    catchUp(id).then(() => markRead(id)).catch(() => {});
  }
}
export function fullSync() {
  const id = S.route.gid;
  update((s) => { s.chats = {}; s.info = {}; });
  refreshGroupsSoon();
  if (id && S.route.name === 'chat') openChat(id).catch(() => {});
}

// ---- markdown ----

const LOCAL_SRC = /^\/(api\/v1\/files|static)\//;
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const md = new Marked({
  gfm: true, breaks: true,
  renderer: {
    html: (t) => esc(t.text),
    image: ({ href, text }) => LOCAL_SRC.test(href || '')
      ? `<img src="${esc(href)}" alt="${esc(text)}" loading="lazy">`
      : `<a href="${esc(href)}">[图片：${esc(text || href)}]</a>`,
  },
});
DOMPurify.addHook('uponSanitizeAttribute', (node, d) => {
  if (d.attrName === 'src' && !LOCAL_SRC.test(d.attrValue)) d.keepAttr = false;
});
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank');
    node.setAttribute('rel', 'noopener noreferrer nofollow');
  }
});
const PURIFY = {
  FORBID_TAGS: ['style', 'form', 'input', 'button', 'textarea', 'select', 'iframe', 'svg', 'math', 'video', 'audio', 'source', 'picture'],
  FORBID_ATTR: ['style', 'srcset', 'class', 'id'],
};
export const renderMd = (s) => DOMPurify.sanitize(md.parse(s || ''), PURIFY);

// ---- utils ----

export function fmtTime(ms) {
  if (!ms) return '';
  const d = new Date(ms), now = new Date();
  const hm = d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
  if (d.toDateString() === now.toDateString()) return hm;
  const md = `${d.getMonth() + 1}/${d.getDate()}`;
  return d.getFullYear() === now.getFullYear() ? `${md} ${hm}` : `${d.getFullYear()}/${md} ${hm}`;
}
export const fmtSize = (n) => n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`;

export async function copy(text) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const t = document.createElement('textarea');
    t.value = text;
    t.style.cssText = 'position:fixed;opacity:0';
    document.body.appendChild(t);
    t.select();
    document.execCommand('copy');
    t.remove();
  }
  toast('已复制');
}

export const randId = () => Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, '0')).join('');

export const ROLE = { owner: '群主', admin: '管理员', member: '成员' };
export const PLATFORM = { claude_code: 'Claude Code', pi: 'pi', other: '其他' };
export const NOT_WOKEN = {
  hop_limit: 'agent 接力已到上限', budget: 'owner 的 agent 唤醒预算已用完',
  sender_limit: '你唤醒它太频繁了', offline: '不在线', disabled: '已停用',
};

export function navigate(hash) {
  if (location.hash === hash) window.dispatchEvent(new HashChangeEvent('hashchange'));
  else location.hash = hash;
}

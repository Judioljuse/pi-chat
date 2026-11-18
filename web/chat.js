import { useState, useEffect, useRef, useLayoutEffect, useMemo } from './vendor/hooks.js';
import {
  html, api, S, update, useStore, toast, fail, chatOf, mergeMsgs, openChat, loadOlder, markRead,
  renderMd, fmtTime, fmtSize, randId, NOT_WOKEN, ROLE, navigate,
} from './lib.js';

const RECALL_MS = 2 * 60 * 1000;
const INLINE_IMG = /^image\/(png|jpeg|gif|webp)$/;
const coarse = matchMedia('(pointer: coarse)');
const fileUrl = (id) => `/api/v1/files/${encodeURIComponent(id)}`;
const snippet = (m) => m ? (m.recalled ? '[已撤回]' : (m.text || (m.files?.length ? '[附件]' : '')).slice(0, 60)) : '';

export function Chat({ gid }) {
  const s = useStore();
  const info = s.info[gid];
  const c = s.chats[gid];
  const [err, setErr] = useState('');
  const [reply, setReply] = useState(null);
  const [active, setActive] = useState(null);
  const [showMembers, setShowMembers] = useState(false);

  useEffect(() => {
    setErr('');
    openChat(gid).catch((e) => setErr(e.message));
  }, [gid]);

  if (err && !c?.loaded) return html`<div class="chat"><${ChatHead} gid=${gid} info=${info} /><div class="empty">${err}</div></div>`;
  if (!info || !c?.loaded) return html`<div class="chat"><${ChatHead} gid=${gid} info=${info} /><div class="empty">加载中…</div></div>`;

  return html`<div class="chat">
    <${ChatHead} gid=${gid} info=${info} onMembers=${() => setShowMembers(!showMembers)} />
    ${showMembers && html`<${MemberPanel} info=${info} onClose=${() => setShowMembers(false)} />`}
    <${MessageList} gid=${gid} info=${info} chat=${c} active=${active} setActive=${setActive}
      onReply=${(m) => { setReply(m); setActive(null); document.querySelector('.composer textarea')?.focus(); }} />
    <${Composer} gid=${gid} info=${info} reply=${reply} clearReply=${() => setReply(null)} />
  </div>`;
}

function ChatHead({ gid, info, onMembers }) {
  const sum = S.groups.find((g) => g.id === gid);
  const name = sum?.name || info?.name || '';
  const isDm = info?.kind === 'dm';
  return html`<header class="bar chat-head">
    <a class="back" href="#/" aria-label="返回">‹</a>
    <div class="title" onClick=${onMembers}>
      <b>${name || (isDm ? '私聊' : '未命名群')}</b>
      ${info && html`<small>${isDm ? (sum?.peer?.kind === 'agent' ? 'agent 私聊' : '私聊') : `${info.members.length} 人${info.topic ? ' · ' + info.topic : ''}`}</small>`}
    </div>
    ${info && !isDm && html`<a class="btn ghost" href=${`#/g/${gid}/settings`}>设置</a>`}
  </header>`;
}

function MemberPanel({ info, onClose }) {
  const startDm = async (pid) => {
    try {
      const { group } = await api('POST', '/chats', { with: pid });
      onClose();
      navigate(`#/g/${group.id}`);
    } catch (e) { fail(e); }
  };
  return html`<div class="member-panel">
    ${info.members.map((m) => html`<div class="row" key=${m.id}>
      <${Who} m=${m} />
      ${m.id !== S.me.id && info.kind !== 'dm' && html`<button class="btn small ghost" onClick=${() => startDm(m.id)}>私聊</button>`}
    </div>`)}
  </div>`;
}

export function Who({ m }) {
  return html`<span class="who">
    ${m.kind === 'agent' && html`<span class=${'dot' + (m.online ? ' on' : '')} title=${m.online ? '在线' : '离线'}></span>`}
    <b>${m.nick || m.displayName}</b>
    ${m.kind === 'agent' && html`<span class="badge agent">agent</span><small class="muted">owner：${m.ownerName || '?'}</small>`}
    ${m.role && m.role !== 'member' && html`<span class="badge">${ROLE[m.role]}</span>`}
  </span>`;
}

function MessageList({ gid, info, chat, active, setActive, onReply }) {
  const ref = useRef();
  const snap = useRef(null);
  const near = useRef(true);
  const first = useRef(true);
  const [newBelow, setNewBelow] = useState(false);
  const msgs = chat.msgs;
  const firstSeq = msgs[0]?.seq, lastSeq = msgs.at(-1)?.seq;

  const el = ref.current;
  if (el) snap.current = { h: el.scrollHeight, top: el.scrollTop, first: snap.current?.first, last: snap.current?.last };

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const prev = snap.current || {};
    if (first.current) {
      first.current = false;
      const div = el.querySelector('.unread-line');
      if (div) div.scrollIntoView({ block: 'start' });
      else el.scrollTop = el.scrollHeight;
    } else if (prev.first && firstSeq < prev.first) {
      el.scrollTop = prev.top + (el.scrollHeight - prev.h);
    } else if (lastSeq !== prev.last) {
      if (near.current || msgs.at(-1)?.author?.id === S.me.id) el.scrollTop = el.scrollHeight;
      else setNewBelow(true);
    }
    snap.current = { ...prev, first: firstSeq, last: lastSeq };
  }, [firstSeq, lastSeq, msgs.length]);

  const onScroll = () => {
    const el = ref.current;
    near.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (near.current && newBelow) setNewBelow(false);
    if (el.scrollTop < 300 && chat.hasMore && !chat.loading) loadOlder(gid).catch(fail);
  };

  const jump = async (seq) => {
    for (let i = 0; i < 20 && chat.msgs[0]?.seq > seq && chat.hasMore; i++) await loadOlder(gid);
    requestAnimationFrame(() => {
      const t = ref.current?.querySelector(`[data-seq="${seq}"]`);
      if (!t) return toast('找不到这条消息');
      t.scrollIntoView({ block: 'center', behavior: 'smooth' });
      t.classList.add('flash');
      setTimeout(() => t.classList.remove('flash'), 1600);
    });
  };

  const bySeq = useMemo(() => new Map(msgs.map((m) => [m.seq, m])), [msgs]);
  const members = info.members;

  return html`<div class="msgs-wrap">
    <div class="msgs" ref=${ref} onScroll=${onScroll}>
      ${chat.hasMore ? html`<div class="more muted">${chat.loading ? '加载中…' : '上滑加载更早的消息'}</div>`
        : html`<div class="more muted">没有更早的消息了</div>`}
      ${msgs.map((m, i) => html`
        ${chat.firstUnread === m.seq && i > 0 && html`<div class="unread-line"><span>以下为新消息</span></div>`}
        <${Msg} key=${m.seq} m=${m} prev=${msgs[i - 1]} gid=${gid} chat=${chat} members=${members}
          quoted=${m.replyTo ? bySeq.get(m.replyTo) || chat.quotes?.[m.replyTo] : null}
          active=${active === m.seq} setActive=${setActive} onReply=${onReply} onJump=${jump} />`)}
    </div>
    ${newBelow && html`<button class="new-below" onClick=${() => { ref.current.scrollTop = ref.current.scrollHeight; setNewBelow(false); }}>新消息 ↓</button>`}
  </div>`;
}

function useQuote(gid, chat, seq, have) {
  useEffect(() => {
    if (!seq || have) return;
    chat.quotes ||= {};
    if (seq in chat.quotes) return;
    chat.quotes[seq] = null;
    api('GET', `/groups/${gid}/messages?after=${seq - 1}&limit=1`)
      .then((r) => { const m = r.messages?.find((x) => x.seq === seq); if (m) update(() => { chat.quotes[seq] = m; }); })
      .catch(() => {});
  }, [seq, have]);
}

function Msg({ m, prev, gid, chat, members, quoted, active, setActive, onReply, onJump }) {
  useQuote(gid, chat, m.replyTo, !!quoted);
  const [showReaders, setShowReaders] = useState(false);
  const [, tick] = useState(0);
  const mine = m.author?.id === S.me.id;
  const canRecall = mine && !m.recalled && Date.now() - m.createdAt < RECALL_MS;
  useEffect(() => {
    if (!canRecall) return;
    const t = setTimeout(() => tick((n) => n + 1), RECALL_MS - (Date.now() - m.createdAt) + 50);
    return () => clearTimeout(t);
  }, [canRecall]);
  const bodyHtml = useMemo(() => (m.text ? renderMd(m.text) : ''), [m.text]);

  if (m.kind === 'system') {
    return html`<div class="sys" data-seq=${m.seq}><span>${m.text}</span> <small>${fmtTime(m.createdAt)}</small></div>`;
  }
  const a = m.author || {};
  const grouped = prev && prev.kind !== 'system' && prev.author?.id === a.id && m.createdAt - prev.createdAt < 5 * 60 * 1000 && !m.replyTo;
  const mentioned = m.mentionAll || m.mentions?.includes(S.me.id);
  const readers = members.filter((x) => x.id !== a.id && x.id !== S.me.id && (x.lastReadSeq || 0) >= m.seq);

  const recall = async () => {
    if (!confirm('撤回这条消息？')) return;
    try {
      await api('DELETE', `/groups/${gid}/messages/${m.seq}`);
      update(() => { m.recalled = true; m.text = ''; m.files = []; });
    } catch (e) { fail(e.code === 'recall_expired' ? { message: '超过 2 分钟，不能撤回了' } : e); }
    setActive(null);
  };

  return html`<div class=${`msg${mine ? ' mine' : ''}${grouped ? ' grouped' : ''}${mentioned ? ' at-me' : ''}${active ? ' active' : ''}`} data-seq=${m.seq}>
    ${!grouped && html`<div class="meta">
      <b class="name">${a.name || '?'}</b>
      ${a.kind === 'agent' && html`<span class="badge agent">agent</span><small class="muted">owner：${a.ownerName || '?'}</small>`}
      <small class="muted">${fmtTime(m.createdAt)}</small>
    </div>`}
    <div class="bubble" onClick=${(e) => { if (!e.target.closest('a,img,button')) setActive(active ? null : m.seq); }}>
      ${m.replyTo && html`<div class="quote" onClick=${(e) => { e.stopPropagation(); onJump(m.replyTo); }}>
        <b>${quoted?.author?.name || '#' + m.replyTo}</b> ${quoted ? snippet(quoted) : '…'}
      </div>`}
      ${m.recalled ? html`<div class="muted recalled">${mine ? '你' : a.name}撤回了一条消息</div>` : html`
        ${bodyHtml && html`<div class="md" dangerouslySetInnerHTML=${{ __html: bodyHtml }}></div>`}
        ${m.files?.length > 0 && html`<div class="files">${m.files.map((f) => INLINE_IMG.test(f.mime)
          ? html`<a href=${fileUrl(f.id)} target="_blank" rel="noopener"><img class="thumb" src=${fileUrl(f.id)} alt=${f.name} loading="lazy" /></a>`
          : html`<a class="file" href=${fileUrl(f.id)} download=${f.name}>📎 ${f.name} <small>${fmtSize(f.size)}</small></a>`)}</div>`}`}
    </div>
    <div class="foot">
      ${grouped && html`<small class="muted">${fmtTime(m.createdAt)}</small>`}
      ${readers.length > 0 && html`<button class="link readers" title=${readers.map((x) => x.nick).join('、')} onClick=${() => setShowReaders(!showReaders)}>
        ✓ ${showReaders ? '已读：' + readers.map((x) => x.nick).join('、') : `${readers.length} 人已读`}</button>`}
      ${active && !m.recalled && html`<span class="acts">
        <button class="link" onClick=${() => onReply(m)}>回复</button>
        ${m.text && html`<button class="link" onClick=${() => { navigator.clipboard?.writeText(m.text).then(() => toast('已复制')); setActive(null); }}>复制</button>`}
        ${canRecall && html`<button class="link danger" onClick=${recall}>撤回</button>`}
      </span>`}
    </div>
  </div>`;
}

function Composer({ gid, info, reply, clearReply }) {
  const [text, setText] = useState(() => sessionStorage.getItem('draft:' + gid) || '');
  const [files, setFiles] = useState([]);
  const [pop, setPop] = useState(null);
  const [sending, setSending] = useState(false);
  const ta = useRef();
  const fileIn = useRef();
  const picked = useRef(new Map());

  useEffect(() => { sessionStorage.setItem('draft:' + gid, text); }, [text]);
  useLayoutEffect(() => {
    const t = ta.current;
    t.style.height = 'auto';
    t.style.height = Math.min(t.scrollHeight, 200) + 'px';
  }, [text]);
  useEffect(() => () => files.forEach((f) => f.url && URL.revokeObjectURL(f.url)), []);

  const candidates = useMemo(() => {
    if (!pop) return [];
    const q = pop.q.toLowerCase();
    const list = info.members.filter((m) => m.id !== S.me.id &&
      ((m.nick || '').toLowerCase().includes(q) || (m.displayName || '').toLowerCase().includes(q)));
    if (['owner', 'admin'].includes(info.myRole) && info.kind === 'group' && 'all'.startsWith(q)) list.unshift({ id: '*all', nick: 'all', all: true });
    return list.slice(0, 8);
  }, [pop?.q, info.members]);

  const detect = (el) => {
    const before = el.value.slice(0, el.selectionStart);
    const mt = /(?:^|\s)@([^\s@]{0,32})$/.exec(before);
    setPop(mt ? { q: mt[1], start: el.selectionStart - mt[1].length - 1, i: 0 } : null);
  };

  const pick = (m) => {
    const el = ta.current;
    const end = el.selectionStart;
    const ins = `@${m.nick} `;
    const v = el.value.slice(0, pop.start) + ins + el.value.slice(end);
    if (!m.all) picked.current.set(m.nick, m.id);
    // Update the DOM synchronously so keystrokes typed right after picking land after the mention.
    el.value = v;
    el.focus();
    el.setSelectionRange(pop.start + ins.length, pop.start + ins.length);
    setText(v);
    setPop(null);
  };

  const upload = async (list) => {
    for (const file of list) {
      if (files.length >= 8) { toast('一条消息最多 8 个附件'); break; }
      if (file.size > 10 * 1024 * 1024) { toast(`${file.name} 超过 10 MB`); continue; }
      const key = randId();
      const url = file.type.startsWith('image/') ? URL.createObjectURL(file) : '';
      setFiles((fs) => [...fs, { key, name: file.name, size: file.size, url, up: true }]);
      try {
        const fd = new FormData();
        fd.append('file', file);
        const { file: f } = await api('POST', '/files', fd);
        setFiles((fs) => fs.map((x) => (x.key === key ? { ...x, ...f, up: false } : x)));
      } catch (e) {
        fail(e);
        setFiles((fs) => fs.filter((x) => x.key !== key));
        url && URL.revokeObjectURL(url);
      }
    }
  };

  const send = async () => {
    const body = text.trim();
    if (sending || (!body && !files.length)) return;
    if (files.some((f) => f.up)) return toast('附件还在上传');
    const mentions = [...picked.current].filter(([nick]) => body.includes('@' + nick)).map(([, id]) => id);
    setSending(true);
    try {
      const r = await api('POST', `/groups/${gid}/messages`, {
        text: body, mentions, replyTo: reply?.seq || undefined, files: files.map((f) => f.id),
        opKey: randId(), ignoreNew: true,
      });
      const c = chatOf(gid);
      update(() => mergeMsgs(c, [r.message]));
      files.forEach((f) => f.url && URL.revokeObjectURL(f.url));
      setText(''); setFiles([]); clearReply(); picked.current.clear();
      reportWake(r, info);
      markRead(gid);
    } catch (e) {
      fail(e.code === 'reply_not_found' ? { message: '被回复的消息不存在或已撤回' } : e);
    } finally { setSending(false); }
  };

  const onKey = (e) => {
    if (pop && candidates.length) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        const n = candidates.length;
        setPop({ ...pop, i: (pop.i + (e.key === 'ArrowDown' ? 1 : n - 1)) % n });
        return;
      }
      if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pick(candidates[pop.i] || candidates[0]); return; }
      if (e.key === 'Escape') { setPop(null); return; }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && !coarse.matches) { e.preventDefault(); send(); }
  };

  return html`<div class="composer">
    ${pop && candidates.length > 0 && html`<ul class="mention-pop">
      ${candidates.map((m, i) => html`<li key=${m.id} class=${i === pop.i ? 'sel' : ''} onMouseDown=${(e) => { e.preventDefault(); pick(m); }}>
        ${m.all ? html`<b>@all</b> <small class="muted">唤醒群里所有 agent</small>` : html`<${Who} m=${m} />`}
      </li>`)}
    </ul>`}
    ${reply && html`<div class="replying"><span>回复 <b>${reply.author?.name}</b>：${snippet(reply)}</span><button class="link" onClick=${clearReply}>×</button></div>`}
    ${files.length > 0 && html`<div class="pending">${files.map((f) => html`<div class="pf" key=${f.key}>
      ${f.url ? html`<img src=${f.url} alt="" />` : html`<span>📎</span>`}
      <small>${f.name}${f.up ? ' 上传中…' : ''}</small>
      <button class="link" onClick=${() => { f.url && URL.revokeObjectURL(f.url); setFiles(files.filter((x) => x.key !== f.key)); }}>×</button>
    </div>`)}</div>`}
    <div class="inputrow">
      <button class="btn ghost icon" title="附件" onClick=${() => fileIn.current.click()}>＋</button>
      <input type="file" multiple hidden ref=${fileIn} onChange=${(e) => { upload([...e.target.files]); e.target.value = ''; }} />
      <textarea ref=${ta} rows="1" value=${text} placeholder=${coarse.matches ? '输入消息' : '输入消息，Enter 发送，Shift+Enter 换行，@ 提及'}
        onInput=${(e) => { setText(e.target.value); detect(e.target); }}
        onClick=${(e) => detect(e.target)} onKeyDown=${onKey} onBlur=${() => setTimeout(() => setPop(null), 150)}
        onPaste=${(e) => { const fs = [...(e.clipboardData?.files || [])]; if (fs.length) { e.preventDefault(); upload(fs); } }}></textarea>
      <button class="btn primary" disabled=${sending} onClick=${send}>发送</button>
    </div>
  </div>`;
}

function reportWake(r, info) {
  const nick = (id) => info.members.find((m) => m.id === id)?.nick || id;
  const notes = [];
  if (r.notWoken?.length) notes.push(r.notWoken.map((x) => `${nick(x.id)} 未唤醒（${NOT_WOKEN[x.reason] || x.reason}）`).join('；'));
  if (r.mentionHint) notes.push(typeof r.mentionHint === 'string' ? r.mentionHint : '有 @ 没有匹配到成员');
  if (notes.length) toast(notes.join('\n'), 6000);
}

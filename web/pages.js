import { useState, useEffect } from './vendor/hooks.js';
import { html, api, S, update, useStore, toast, fail, loadInfo, loadAgents, loadGroups, copy, fmtTime, PLATFORM, navigate } from './lib.js';
import { Who } from './chat.js';

const Field = ({ label, children, hint }) => html`<label class="field"><span>${label}</span>${children}${hint && html`<small class="muted">${hint}</small>`}</label>`;
const Page = ({ title, back = '#/', children, actions }) => html`<div class="page">
  <header class="bar"><a class="back" href=${back} aria-label="返回">‹</a><div class="title"><b>${title}</b></div>${actions}</header>
  <div class="page-body">${children}</div>
</div>`;

function useAsync(fn, deps) {
  const [st, set] = useState({ loading: true });
  const run = () => fn().then((data) => set({ data }), (error) => set({ error }));
  useEffect(() => { set({ loading: true }); run(); }, deps);
  return [st, run];
}

// ---- auth ----

export function Login() {
  const [f, setF] = useState({ name: '', password: '' });
  const [busy, setBusy] = useState(false);
  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    try {
      const { user } = await api('POST', '/auth/login', f);
      window.dispatchEvent(new CustomEvent('pichat:login', { detail: user }));
    } catch (e) { fail(e.status === 401 ? { message: '用户名或密码错误' } : e); } finally { setBusy(false); }
  };
  return html`<div class="auth"><form class="card" onSubmit=${submit}>
    <h1>pichat</h1>
    <${Field} label="用户名"><input autocomplete="username" required value=${f.name} onInput=${(e) => setF({ ...f, name: e.target.value })} /></${Field}>
    <${Field} label="密码"><input type="password" autocomplete="current-password" required value=${f.password} onInput=${(e) => setF({ ...f, password: e.target.value })} /></${Field}>
    <button class="btn primary block" disabled=${busy}>登录</button>
    <p class="muted small">没有账号？请向管理员要邀请链接。</p>
  </form></div>`;
}

export function Redeem({ code }) {
  const s = useStore();
  const [st] = useAsync(() => api('GET', `/auth/invite/${encodeURIComponent(code)}`), [code]);
  const [f, setF] = useState({ name: '', displayName: '', password: '', password2: '' });
  const [busy, setBusy] = useState(false);
  const inv = st.data;

  const redeem = async (body) => {
    setBusy(true);
    try {
      const r = await api('POST', '/auth/redeem', { code, ...body });
      window.dispatchEvent(new CustomEvent('pichat:login', { detail: r.user, }));
      if (r.groupId) setTimeout(() => navigate(`#/g/${r.groupId}`), 50);
      else navigate('#/');
    } catch (e) { fail(e.code === 'conflict' ? { message: '用户名已被占用' } : e); } finally { setBusy(false); }
  };
  const submit = (e) => {
    e.preventDefault();
    if (!/^[a-z0-9_-]{3,32}$/.test(f.name)) return toast('用户名只能用小写字母、数字、_ 和 -，3 到 32 位');
    if (f.password.length < 8) return toast('密码至少 8 位');
    if (f.password !== f.password2) return toast('两次密码不一致');
    redeem({ name: f.name, displayName: f.displayName || f.name, password: f.password });
  };
  const goLogin = () => { sessionStorage.setItem('afterLogin', location.hash); navigate('#/login'); };

  if (st.loading) return html`<div class="auth"><div class="card">加载中…</div></div>`;
  if (st.error) return html`<div class="auth"><div class="card"><h2>链接无效</h2><p class="muted">邀请链接不存在、已过期或已用完。</p><a class="btn" href="#/">返回</a></div></div>`;

  const title = inv.kind === 'group' ? `加入群「${inv.group?.name || ''}」` : inv.kind === 'admin' ? '创建管理员账号' : '注册 pichat';
  return html`<div class="auth"><div class="card">
    <h2>${title}</h2>
    ${inv.group?.topic && html`<p class="muted">${inv.group.topic}</p>`}
    ${inv.kind === 'group' && s.me ? html`
      <p>你当前登录为 <b>${s.me.displayName}</b>。</p>
      <button class="btn primary block" disabled=${busy} onClick=${() => redeem({})}>加入群</button>` : html`
      <form onSubmit=${submit}>
        <${Field} label="用户名" hint="小写字母、数字、_ -，3-32 位，用于登录"><input autocomplete="username" required value=${f.name} onInput=${(e) => setF({ ...f, name: e.target.value.trim().toLowerCase() })} /></${Field}>
        <${Field} label="显示名"><input value=${f.displayName} placeholder=${f.name} onInput=${(e) => setF({ ...f, displayName: e.target.value })} /></${Field}>
        <${Field} label="密码" hint="至少 8 位"><input type="password" autocomplete="new-password" required value=${f.password} onInput=${(e) => setF({ ...f, password: e.target.value })} /></${Field}>
        <${Field} label="确认密码"><input type="password" autocomplete="new-password" required value=${f.password2} onInput=${(e) => setF({ ...f, password2: e.target.value })} /></${Field}>
        <button class="btn primary block" disabled=${busy}>${inv.kind === 'group' ? '注册并加入' : '注册'}</button>
      </form>
      ${inv.kind === 'group' && html`<p class="small">已有账号？<button class="link" onClick=${goLogin}>登录后加入</button></p>`}`}
  </div></div>`;
}

// ---- profile ----

export function Me() {
  const s = useStore();
  const [dn, setDn] = useState(s.me.displayName);
  const [pw, setPw] = useState({ cur: '', a: '', b: '' });
  const save = async (body, done) => {
    try {
      const r = await api('PATCH', '/me', body);
      update((st) => { st.me = r.me || { ...st.me, ...body, password: undefined, currentPassword: undefined }; });
      toast('已保存');
      done?.();
    } catch (e) { fail(e); }
  };
  const logout = async () => {
    try { await api('POST', '/auth/logout', {}); } catch { /* ignore */ }
    window.dispatchEvent(new Event('pichat:logout'));
  };
  return html`<${Page} title="我的账号">
    <section class="card">
      <p class="muted">登录名：${s.me.name}${s.me.isAdmin ? '（管理员）' : ''}</p>
      <${Field} label="显示名"><input value=${dn} onInput=${(e) => setDn(e.target.value)} /></${Field}>
      <button class="btn" onClick=${() => save({ displayName: dn.trim() })} disabled=${!dn.trim() || dn === s.me.displayName}>保存</button>
    </section>
    <section class="card">
      <h3>修改密码</h3>
      <${Field} label="当前密码"><input type="password" autocomplete="current-password" value=${pw.cur} onInput=${(e) => setPw({ ...pw, cur: e.target.value })} /></${Field}>
      <${Field} label="新密码"><input type="password" autocomplete="new-password" value=${pw.a} onInput=${(e) => setPw({ ...pw, a: e.target.value })} /></${Field}>
      <${Field} label="确认新密码"><input type="password" autocomplete="new-password" value=${pw.b} onInput=${(e) => setPw({ ...pw, b: e.target.value })} /></${Field}>
      <button class="btn" onClick=${() => {
        if (pw.a.length < 8) return toast('密码至少 8 位');
        if (pw.a !== pw.b) return toast('两次密码不一致');
        if (!pw.cur) return toast('请输入当前密码');
        save({ password: pw.a, currentPassword: pw.cur }, () => setPw({ cur: '', a: '', b: '' }));
      }}>修改密码</button>
    </section>
    <section class="card"><button class="btn danger" onClick=${logout}>退出登录</button></section>
  </${Page}>`;
}

// ---- group settings ----

export function GroupSettings({ gid }) {
  const s = useStore();
  const info = s.info[gid];
  const [form, setForm] = useState(null);
  const [invite, setInvite] = useState(null);

  useEffect(() => { loadInfo(gid).catch(fail); loadAgents().catch(() => {}); }, [gid]);
  useEffect(() => { if (info && !form) setForm({ name: info.name, topic: info.topic }); }, [info]);
  if (!info || !form) return html`<${Page} title="群设置" back=${`#/g/${gid}`}><div class="empty">加载中…</div></${Page}>`;

  const isOwner = info.myRole === 'owner';
  const manage = isOwner || info.myRole === 'admin';
  const isDm = info.kind === 'dm';
  const memberIds = new Set(info.members.map((m) => m.id));
  const myAgents = (s.agents || []).filter((a) => !memberIds.has(a.id));

  const act = async (fn, okMsg) => {
    try { await fn(); okMsg && toast(okMsg); await loadInfo(gid).catch(() => {}); } catch (e) { fail(e); }
  };
  const saveInfo = () => act(() => api('PATCH', `/groups/${gid}`, { name: form.name.trim(), topic: form.topic.trim() }).then(loadGroups), '已保存');
  const genInvite = () => act(async () => { setInvite(await api('POST', `/groups/${gid}/invite`, {})); });
  const kick = (m) => confirm(`把 ${m.nick} 移出群？${m.kind === 'human' ? '他名下的 agent 也会一起移出。' : ''}`) &&
    act(() => api('DELETE', `/groups/${gid}/members/${m.id}`), '已移出');
  const setRole = (m, role) => act(() => api('PATCH', `/groups/${gid}/members/${m.id}`, { role }));
  const addAgent = (a) => act(() => api('POST', `/groups/${gid}/members`, { agentId: a.id }), `已把 ${a.displayName} 拉进群`);
  const dm = async (m) => {
    try { const { group } = await api('POST', '/chats', { with: m.id }); navigate(`#/g/${group.id}`); } catch (e) { fail(e); }
  };
  const leave = async () => {
    if (!confirm(isOwner ? '你是群主。退出后群会转给最早加入的人类；没有其他人类时群会解散。确定退出？' : '确定退出这个群？')) return;
    try { await api('DELETE', `/groups/${gid}/members/${s.me.id}`); await loadGroups(); navigate('#/'); } catch (e) { fail(e); }
  };
  const dissolve = async () => {
    if (!confirm('解散后群和全部消息都会删除，无法恢复。确定解散？')) return;
    try { await api('DELETE', `/groups/${gid}`); await loadGroups(); navigate('#/'); } catch (e) { fail(e); }
  };

  return html`<${Page} title=${isDm ? '私聊信息' : '群设置'} back=${`#/g/${gid}`}>
    ${!isDm && html`<section class="card">
      <${Field} label="群名"><input value=${form.name} disabled=${!manage} onInput=${(e) => setForm({ ...form, name: e.target.value })} /></${Field}>
      <${Field} label="话题"><textarea rows="2" value=${form.topic} disabled=${!manage} onInput=${(e) => setForm({ ...form, topic: e.target.value })}></textarea></${Field}>
      ${manage && html`<button class="btn" disabled=${!form.name.trim()} onClick=${saveInfo}>保存</button>`}
    </section>`}

    ${manage && !isDm && html`<section class="card">
      <h3>邀请链接</h3>
      <p class="muted small">7 天有效，最多 20 次。生成新链接会作废旧链接。未注册的人可以用它注册并入群。</p>
      ${invite && html`<div class="copyrow"><code>${invite.url}</code><button class="btn small" onClick=${() => copy(invite.url)}>复制</button></div>`}
      <button class="btn" onClick=${genInvite}>${invite ? '重新生成' : '生成邀请链接'}</button>
    </section>`}

    <section class="card">
      <h3>成员（${info.members.length}）</h3>
      ${info.members.map((m) => html`<div class="row" key=${m.id}>
        <${Who} m=${m} />
        <span class="row-acts">
          ${m.id !== s.me.id && !isDm && html`<button class="btn small ghost" onClick=${() => dm(m)}>私聊</button>`}
          ${isOwner && m.kind === 'human' && m.id !== s.me.id && (m.role === 'admin'
            ? html`<button class="btn small ghost" onClick=${() => setRole(m, 'member')}>取消管理员</button>`
            : html`<button class="btn small ghost" onClick=${() => setRole(m, 'admin')}>设为管理员</button>`)}
          ${manage && !isDm && m.role !== 'owner' && m.id !== s.me.id && (isOwner || m.role !== 'admin') &&
            html`<button class="btn small ghost danger" onClick=${() => kick(m)}>移出</button>`}
          ${!isDm && m.kind === 'agent' && m.ownerId === s.me.id && !manage && html`<button class="btn small ghost danger" onClick=${() => kick(m)}>移出</button>`}
        </span>
      </div>`)}
    </section>

    ${!isDm && html`<section class="card">
      <h3>拉我的 agent 进群</h3>
      ${s.agents == null ? html`<p class="muted">加载中…</p>` : myAgents.length === 0
        ? html`<p class="muted small">没有可以拉进来的 agent。<a href="#/agents">去添加</a></p>`
        : myAgents.map((a) => html`<div class="row" key=${a.id}>
            <span class="who"><span class=${'dot' + (a.online ? ' on' : '')}></span><b>${a.displayName}</b><small class="muted">${PLATFORM[a.platform] || a.platform || ''}</small></span>
            <button class="btn small" onClick=${() => addAgent(a)}>拉进群</button>
          </div>`)}
    </section>`}

    ${!isDm && html`<section class="card danger-zone">
      <button class="btn danger" onClick=${leave}>退出群</button>
      ${isOwner && html`<button class="btn danger" onClick=${dissolve}>解散群</button>`}
    </section>`}
  </${Page}>`;
}

// ---- agents ----

export function Agents() {
  const s = useStore();
  const [pair, setPair] = useState(null);
  const [pairGroup, setPairGroup] = useState('');
  useEffect(() => { loadAgents().catch(fail); if (!s.groupsLoaded) loadGroups().catch(() => {}); }, []);
  const groups = s.groups.filter((g) => g.kind === 'group');

  const newCode = async () => {
    try { setPair(await api('POST', '/pairing-codes', pairGroup ? { groupId: pairGroup } : {})); } catch (e) { fail(e); }
  };

  return html`<${Page} title="我的 agent" actions=${html`<button class="btn ghost" onClick=${() => loadAgents().catch(fail)}>刷新</button>`}>
    <section class="card">
      <h3>添加 agent</h3>
      <p class="muted small">生成配对码，把下面这段话发给你的 agent（Claude Code 或 pi），它会自己完成接入。配对码 30 分钟内有效，只能用一次。</p>
      <${Field} label="配对后自动加入群（可选）">
        <select value=${pairGroup} onChange=${(e) => setPairGroup(e.target.value)}>
          <option value="">不加入</option>
          ${groups.map((g) => html`<option value=${g.id}>${g.name}</option>`)}
        </select>
      </${Field}>
      <button class="btn primary" onClick=${newCode}>${pair ? '重新生成' : '生成配对码'}</button>
      ${pair && html`<${CodeBox} code=${pair.code} instruction=${pair.instruction} expiresAt=${pair.expiresAt} />`}
    </section>
    ${s.agents == null ? html`<div class="empty">加载中…</div>` : s.agents.length === 0
      ? html`<div class="empty">还没有 agent。</div>`
      : s.agents.map((a) => html`<${AgentCard} key=${a.id} a=${a} />`)}
  </${Page}>`;
}

function CodeBox({ code, instruction, expiresAt }) {
  return html`<div class="codebox">
    <div class="copyrow"><span>配对码 <code class="big">${code}</code></span><button class="btn small" onClick=${() => copy(code)}>复制</button></div>
    ${expiresAt && html`<small class="muted">${fmtTime(expiresAt)} 前有效</small>`}
    ${instruction && html`<pre class="instr">${instruction}</pre><button class="btn small" onClick=${() => copy(instruction)}>复制给 agent 的话</button>`}
  </div>`;
}

function AgentCard({ a }) {
  const s = useStore();
  const [notes, setNotes] = useState(a.ownerNotes || '');
  const [name, setName] = useState(a.displayName);
  const [reset, setReset] = useState(null);
  const [wake, setWake] = useState({ kind: a.wake?.kind || 'listener', url: a.wake?.url || '' });
  const [secret, setSecret] = useState('');
  useEffect(() => { setNotes(a.ownerNotes || ''); setName(a.displayName); }, [a.ownerNotes, a.displayName]);
  useEffect(() => { setWake({ kind: a.wake?.kind || 'listener', url: a.wake?.url || '' }); }, [a.wake?.kind, a.wake?.url]);

  const patch = async (body, msg = '已保存') => {
    try {
      const { agent } = await api('PATCH', `/agents/${a.id}`, body);
      update((st) => { st.agents = st.agents.map((x) => (x.id === a.id ? { ...x, ...(agent || body) } : x)); });
      toast(msg);
    } catch (e) { fail(e); }
  };
  const setTier = (tier) => {
    if (tier === a.commanderTier) return;
    if (tier === 'full' && !confirm(`把「${a.displayName}」设为 full 档？\n\n指挥者 @ 它时，它会拥有所在机器上当前用户的完整权限：读写全部目录、执行任意命令。请只授权你信任的人。`)) return;
    patch({ commanderTier: tier }, tier === 'full' ? '已切换到 full' : '已切换到 project');
  };
  const saveWake = async () => {
    if (wake.kind === 'webhook' && !/^https?:\/\//.test(wake.url)) return toast('webhook 地址必须是 http(s)');
    try {
      const r = await api('PUT', `/agents/${a.id}/wake`, wake.kind === 'webhook' ? wake : { kind: wake.kind });
      setSecret(r.secret || '');
      await loadAgents();
      toast('唤醒方式已保存');
    } catch (e) { fail(e); }
  };
  const resetToken = async () => {
    if (!confirm('重置后旧 token 立即失效，需要用新的配对码重新接入。继续？')) return;
    try { setReset(await api('POST', `/agents/${a.id}/reset-token`, {})); } catch (e) { fail(e); }
  };
  const del = async () => {
    if (!confirm(`删除「${a.displayName}」？会吊销它的 token 并把它移出所有群。`)) return;
    try { await api('DELETE', `/agents/${a.id}`); await loadAgents(); toast('已删除'); } catch (e) { fail(e); }
  };

  return html`<section class="card agent-card">
    <div class="agent-head">
      <span class=${'dot' + (a.online ? ' on' : '')}></span>
      <b>${a.displayName}</b>
      <span class="badge">${PLATFORM[a.platform] || a.platform || '未知平台'}</span>
      <small class="muted">${a.online ? '在线' : a.lastSeenAt ? '最后在线 ' + fmtTime(a.lastSeenAt) : '从未上线'}</small>
      ${a.disabled && html`<span class="badge danger">已停用</span>`}
    </div>
    ${a.intro && html`<p class="muted small">${a.intro}</p>`}

    <details>
      <summary>名字</summary>
      <div class="inline"><input value=${name} onInput=${(e) => setName(e.target.value)} />
        <button class="btn small" disabled=${!name.trim() || name === a.displayName} onClick=${() => patch({ displayName: name.trim() })}>改名</button></div>
    </details>

    <div class="field"><span>指挥档位</span>
      <div class="seg">
        <button class=${a.commanderTier !== 'full' ? 'on' : ''} onClick=${() => setTier('project')}>project</button>
        <button class=${a.commanderTier === 'full' ? 'on danger' : ''} onClick=${() => setTier('full')}>full</button>
      </div>
      <small class="muted">${a.commanderTier === 'full' ? '指挥者 @ 它时：完整权限，读写全部目录、执行任意命令。' : '指挥者 @ 它时：只在绑定的项目目录里工作。'}其他人 @ 它只能让它回话。</small>
    </div>

    <div class="field"><span>长期说明（写给 agent，${notes.length}/2000）</span>
      <textarea rows="3" maxlength="2000" value=${notes} onInput=${(e) => setNotes(e.target.value)} placeholder="例如：回复保持简短；改代码前先说明计划"></textarea>
      <div><button class="btn small" disabled=${notes === (a.ownerNotes || '')} onClick=${() => patch({ ownerNotes: notes })}>保存说明</button></div>
    </div>

    <${Grants} a=${a} />

    <div class="field"><span>唤醒方式</span>
      <div class="inline">
        <select value=${wake.kind} onChange=${(e) => setWake({ ...wake, kind: e.target.value })}>
          <option value="listener">listener（本机常驻）</option>
          <option value="webhook">webhook</option>
          <option value="none">不唤醒</option>
        </select>
        ${wake.kind === 'webhook' && html`<input placeholder="https://..." value=${wake.url} onInput=${(e) => setWake({ ...wake, url: e.target.value })} />`}
        <button class="btn small" onClick=${saveWake}>保存</button>
      </div>
      ${a.wake?.failStreak > 0 && html`<small class="danger">连续失败 ${a.wake.failStreak} 次</small>`}
      ${a.wake?.lastError && html`<small class="danger">最近错误：${a.wake.lastError}</small>`}
      ${secret && html`<div class="copyrow"><span class="small">webhook 密钥（只显示这一次）：<code>${secret}</code></span><button class="btn small" onClick=${() => copy(secret)}>复制</button></div>`}
    </div>

    <details>
      <summary>最近运行（${a.recentRuns?.length || 0}）</summary>
      ${a.recentRuns?.length ? html`<ul class="runs">${a.recentRuns.map((r) => html`<li>
        <span class=${r.ok ? 'ok' : 'danger'}>${r.ok ? '✓' : '✗'}</span>
        <small class="muted">${fmtTime(r.createdAt)}</small>
        <small>${s.groups.find((g) => g.id === r.groupId)?.name || r.groupId || ''}</small>
        ${r.detail && html`<div class="small muted pre">${r.detail}</div>`}
      </li>`)}</ul>` : html`<p class="muted small">暂无记录</p>`}
    </details>

    ${reset && html`<${CodeBox} code=${reset.code} instruction=${reset.instruction} />`}
    <div class="inline">
      <button class="btn small" onClick=${() => patch({ disabled: !a.disabled }, a.disabled ? '已启用' : '已停用')}>${a.disabled ? '启用' : '停用'}</button>
      <button class="btn small" onClick=${resetToken}>重置 token</button>
      <button class="btn small danger" onClick=${del}>删除</button>
    </div>
  </section>`;
}

function Grants({ a }) {
  const s = useStore();
  const [gid, setGid] = useState('');
  const [uid, setUid] = useState('');
  const [busy, setBusy] = useState(false);
  const groups = s.groups.filter((g) => g.kind === 'group');
  useEffect(() => { if (gid && gid !== '*' && !s.info[gid]) loadInfo(gid).catch(() => {}); setUid(''); }, [gid]);
  useEffect(() => { if (gid === '*') groups.forEach((g) => { if (!s.info[g.id]) loadInfo(g.id).catch(() => {}); }); }, [gid]);

  const humans = (() => {
    const src = gid === '*' ? groups.map((g) => s.info[g.id]).filter(Boolean) : gid ? [s.info[gid]].filter(Boolean) : [];
    const m = new Map();
    src.forEach((g) => g.members.forEach((x) => { if (x.kind === 'human' && x.id !== s.me.id) m.set(x.id, x); }));
    return [...m.values()];
  })();

  const userName = (g) => g.userId === '*' ? '所有人类' : g.userName || g.userDisplayName || findHuman(s, g.userId) || g.userId;
  const groupName = (g) => g.groupId === '*' ? '所有群' : g.groupName || s.groups.find((x) => x.id === g.groupId)?.name || g.groupId;

  const change = async (method, body) => {
    setBusy(true);
    try {
      const { grants } = await api(method, `/agents/${a.id}/grants`, body);
      update((st) => { st.agents = st.agents.map((x) => (x.id === a.id ? { ...x, grants } : x)); });
      if (method === 'PUT') { setUid(''); toast('已授权'); }
    } catch (e) { fail(e); } finally { setBusy(false); }
  };

  return html`<div class="field"><span>指挥者授权</span>
    <small class="muted">你本人永远是指挥者。被授权的人在对应的群里 @ 它时，它会按上面的档位执行任务。</small>
    ${a.grants?.length > 0 ? html`<ul class="grants">${a.grants.map((g) => html`<li>
      <span><b>${userName(g)}</b> <small class="muted">在</small> ${groupName(g)}</span>
      <button class="link danger" disabled=${busy} onClick=${() => change('DELETE', { userId: g.userId, groupId: g.groupId })}>撤销</button>
    </li>`)}</ul>` : html`<p class="muted small">暂无授权</p>`}
    <div class="inline">
      <select value=${gid} onChange=${(e) => setGid(e.target.value)}>
        <option value="">选择群</option>
        ${groups.map((g) => html`<option value=${g.id}>${g.name}</option>`)}
        <option value="*">所有群</option>
      </select>
      <select value=${uid} disabled=${!gid} onChange=${(e) => setUid(e.target.value)}>
        <option value="">选择人</option>
        ${gid && gid !== '*' && html`<option value="*">该群所有人类</option>`}
        ${humans.map((m) => html`<option value=${m.id}>${m.displayName || m.nick}</option>`)}
      </select>
      <button class="btn small" disabled=${!gid || !uid || busy} onClick=${() => change('PUT', { userId: uid, groupId: gid })}>授权</button>
    </div>
  </div>`;
}

function findHuman(s, id) {
  for (const g of Object.values(s.info)) {
    const m = g.members?.find((x) => x.id === id);
    if (m) return m.displayName || m.nick;
  }
  return '';
}

// ---- admin ----

export function Admin() {
  const s = useStore();
  const [inv, setInv] = useState(null);
  const [st, reload] = useAsync(() => api('GET', '/admin/users'), []);
  if (!s.me.isAdmin) return html`<${Page} title="管理"><div class="empty">只有管理员可以访问</div></${Page}>`;
  const gen = async () => { try { setInv(await api('POST', '/admin/invites', {})); } catch (e) { fail(e); } };
  return html`<${Page} title="管理">
    <section class="card">
      <h3>用户邀请链接</h3>
      <p class="muted small">24 小时内有效，只能注册 1 个账号。</p>
      ${inv && html`<div class="copyrow"><code>${inv.url}</code><button class="btn small" onClick=${() => copy(inv.url)}>复制</button></div>`}
      <button class="btn primary" onClick=${gen}>生成邀请链接</button>
    </section>
    <section class="card">
      <h3>用户 <button class="link" onClick=${reload}>刷新</button></h3>
      ${st.loading ? html`<p class="muted">加载中…</p>` : st.error ? html`<p class="danger">${st.error.message}</p>`
        : html`<table class="users"><thead><tr><th>登录名</th><th>显示名</th><th>角色</th><th>注册时间</th></tr></thead><tbody>
          ${st.data.users.map((u) => html`<tr key=${u.id}><td>${u.name}</td><td>${u.displayName}</td><td>${u.isAdmin ? '管理员' : '用户'}</td><td>${fmtTime(u.createdAt)}</td></tr>`)}
        </tbody></table>`}
    </section>
  </${Page}>`;
}

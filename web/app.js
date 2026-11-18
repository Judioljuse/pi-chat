import { render } from './vendor/preact.js';
import { useState, useEffect } from './vendor/hooks.js';
import {
  html, api, S, update, useStore, toast, fail, loadGroups, connectEvents, closeEvents, eventsAlive,
  softSync, setUnauthorizedHandler, fmtTime, navigate,
} from './lib.js';
import { Chat } from './chat.js';
import { Login, Redeem, Me, GroupSettings, Agents, Admin } from './pages.js';

function parseRoute() {
  const p = location.hash.replace(/^#\/?/, '').split('/').map(decodeURIComponent);
  switch (p[0]) {
    case 'login': return { name: 'login' };
    case 'redeem': return { name: 'redeem', code: p[1] || '' };
    case 'g': return p[1] ? { name: p[2] === 'settings' ? 'settings' : 'chat', gid: p[1] } : { name: 'home' };
    case 'agents': return { name: 'agents' };
    case 'admin': return { name: 'admin' };
    case 'me': return { name: 'me' };
    default: return { name: 'home' };
  }
}
const PUBLIC = new Set(['login', 'redeem']);

function GroupList() {
  const s = useStore();
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const create = async (e) => {
    e.preventDefault();
    if (!name.trim()) return;
    try {
      const { group } = await api('POST', '/groups', { name: name.trim() });
      setName(''); setCreating(false);
      await loadGroups();
      navigate(`#/g/${group.id}`);
    } catch (e) { fail(e); }
  };
  return html`<aside class="sidebar">
    <header class="bar">
      <div class="title"><b>pichat</b></div>
      <button class="btn ghost" title="新建群" onClick=${() => setCreating(!creating)}>＋</button>
    </header>
    ${creating && html`<form class="newgroup" onSubmit=${create}>
      <input autofocus placeholder="群名" value=${name} onInput=${(e) => setName(e.target.value)} />
      <button class="btn primary small">创建</button>
    </form>`}
    <nav class="glist">
      ${!s.groupsLoaded ? html`<div class="empty">加载中…</div>` : s.groups.length === 0
        ? html`<div class="empty">还没有群。点右上角 ＋ 建一个。</div>`
        : s.groups.map((g) => html`<a key=${g.id} href=${`#/g/${g.id}`} class=${'gitem' + (s.route.gid === g.id ? ' cur' : '')}>
          <span class=${'avatar' + (g.kind === 'dm' ? ' dm' : '') + (g.peer?.kind === 'agent' ? ' ag' : '')}>${(g.name || '?').slice(0, 1).toUpperCase()}</span>
          <span class="gmain">
            <span class="gtop"><b class="gname">${g.name || (g.kind === 'dm' ? '私聊' : '未命名群')}</b>
              <small class="muted">${fmtTime(g.lastMessage?.createdAt)}</small></span>
            <span class="gbot"><small class="muted gsnip">${g.lastMessage ? `${g.kind === 'dm' ? '' : g.lastMessage.authorName + '：'}${g.lastMessage.text}` : g.topic}</small>
              ${g.mentionsMe > 0 && html`<span class="pill at">@我</span>`}
              ${g.unread > 0 && html`<span class="pill">${g.unread > 99 ? '99+' : g.unread}</span>`}</span>
          </span>
        </a>`)}
    </nav>
    <footer class="side-foot">
      <a href="#/agents" class=${s.route.name === 'agents' ? 'cur' : ''}>我的 agent</a>
      ${s.me?.isAdmin && html`<a href="#/admin" class=${s.route.name === 'admin' ? 'cur' : ''}>管理</a>`}
      <a href="#/me" class=${s.route.name === 'me' ? 'cur' : ''}>${s.me?.displayName || '我'}</a>
    </footer>
  </aside>`;
}

function Main() {
  const { route } = S;
  switch (route.name) {
    case 'chat': return html`<${Chat} key=${route.gid} gid=${route.gid} />`;
    case 'settings': return html`<${GroupSettings} key=${route.gid} gid=${route.gid} />`;
    case 'agents': return html`<${Agents} />`;
    case 'admin': return html`<${Admin} />`;
    case 'me': return html`<${Me} />`;
    default: return html`<div class="empty welcome">选择一个群开始聊天</div>`;
  }
}

function App() {
  const s = useStore();
  const [boot, setBoot] = useState(true);

  useEffect(() => {
    const onHash = () => {
      const r = parseRoute();
      if (!s.me && !PUBLIC.has(r.name)) { sessionStorage.setItem('afterLogin', location.hash); location.hash = '#/login'; return; }
      if (s.me && r.name === 'login') { location.hash = '#/'; return; }
      update((st) => { st.route = r; });
    };
    const login = (me) => {
      update((st) => { st.me = me; });
      loadGroups().catch(fail);
      connectEvents();
    };
    const onLogin = async (e) => {
      let me = e.detail;
      try { me = await api('GET', '/me'); } catch { /* use login payload */ }
      login(me);
      const next = sessionStorage.getItem('afterLogin');
      sessionStorage.removeItem('afterLogin');
      if (parseRoute().name === 'login') location.hash = next && !next.startsWith('#/login') ? next : '#/';
      else onHash();
    };
    const onLogout = () => {
      closeEvents();
      update((st) => { Object.assign(st, { me: null, groups: [], groupsLoaded: false, info: {}, chats: {}, agents: null }); });
      location.hash = '#/login';
    };
    setUnauthorizedHandler(() => { if (S.me) { toast('登录已过期，请重新登录'); onLogout(); } });
    const onVis = () => {
      if (document.visibilityState !== 'visible' || !S.me) return;
      if (!eventsAlive()) connectEvents();
      softSync();
    };

    window.addEventListener('hashchange', onHash);
    window.addEventListener('pichat:login', onLogin);
    window.addEventListener('pichat:logout', onLogout);
    document.addEventListener('visibilitychange', onVis);
    window.addEventListener('online', onVis);

    api('GET', '/me').then((me) => { if (me.kind === 'human') login(me); }).catch(() => {}).finally(() => { setBoot(false); onHash(); });
    return () => {
      window.removeEventListener('hashchange', onHash);
      window.removeEventListener('pichat:login', onLogin);
      window.removeEventListener('pichat:logout', onLogout);
      document.removeEventListener('visibilitychange', onVis);
      window.removeEventListener('online', onVis);
    };
  }, []);

  useEffect(() => {
    const unread = s.groups.reduce((n, g) => n + (g.unread || 0), 0);
    document.title = unread ? `(${unread}) pichat` : 'pichat';
  }, [s.groups]);

  let body;
  if (boot) body = html`<div class="empty">加载中…</div>`;
  else if (s.route.name === 'login') body = html`<${Login} />`;
  else if (s.route.name === 'redeem') body = html`<${Redeem} code=${s.route.code} />`;
  else if (!s.me) body = html`<div class="empty">加载中…</div>`;
  else body = html`<div class=${'shell' + (s.route.name === 'home' ? ' at-home' : ' at-main')}>
    <${GroupList} />
    <main class="main"><${Main} /></main>
  </div>`;

  return html`${body}<div class="toasts">${s.toasts.map((t) => html`<div class="toast" key=${t.id}>${t.msg}</div>`)}</div>`;
}

render(html`<${App} />`, document.getElementById('app'));

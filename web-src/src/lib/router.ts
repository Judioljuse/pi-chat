import { getState, setState, type Route } from './store'

export const PUBLIC_ROUTES = new Set<Route['name']>(['login', 'redeem'])

/** "#/g/<id>?seq=12": the message to scroll to once the chat is open (deep link from a 「群名」#12 reference). */
export function routeSeq(hash = location.hash): number | null {
  const q = hash.indexOf('?')
  if (q < 0) return null
  const v = new URLSearchParams(hash.slice(q + 1)).get('seq')
  return v && /^[1-9]\d{0,8}$/.test(v) ? Number(v) : null
}

/** "#/g/<id>?tasks": open the task panel once the chat is open. */
export function routeTasks(hash = location.hash): boolean {
  const q = hash.indexOf('?')
  return q >= 0 && new URLSearchParams(hash.slice(q + 1)).has('tasks')
}

export function parseRoute(hash = location.hash): Route {
  const p = hash.replace(/^#\/?/, '').replace(/\?.*$/, '').split('/').map((x) => {
    try {
      return decodeURIComponent(x)
    } catch {
      return x
    }
  })
  switch (p[0]) {
    case 'login':
      return { name: 'login' }
    case 'redeem':
      return { name: 'redeem', code: p[1] || '' }
    case 'g':
      if (!p[1]) return { name: 'home' }
      return p[2] === 'settings' ? { name: 'settings', gid: p[1] } : { name: 'chat', gid: p[1] }
    case 'agents':
      return p[1] ? { name: 'agents', focus: p[1] } : { name: 'agents' }
    case 'admin':
      return { name: 'admin' }
    case 'me':
      return { name: 'me' }
    case 'tasks':
      return { name: 'tasks' }
    case 'decisions':
      return { name: 'decisions' }
    case 'usage':
      return { name: 'usage' }
    default:
      return { name: 'home' }
  }
}

export function navigate(hash: string) {
  if (location.hash === hash) window.dispatchEvent(new HashChangeEvent('hashchange'))
  else location.hash = hash
}

const AFTER_LOGIN = 'afterLogin'

export const rememberAfterLogin = (hash = location.hash) => sessionStorage.setItem(AFTER_LOGIN, hash)

export function takeAfterLogin(): string {
  const next = sessionStorage.getItem(AFTER_LOGIN)
  sessionStorage.removeItem(AFTER_LOGIN)
  return next && !next.startsWith('#/login') ? next : '#/'
}

/** Apply the current hash to the store, redirecting around the login wall. */
export function syncRoute() {
  const r = parseRoute()
  const { me } = getState()
  if (!me && !PUBLIC_ROUTES.has(r.name)) {
    rememberAfterLogin()
    location.hash = '#/login'
    return
  }
  if (me && r.name === 'login') {
    location.hash = '#/'
    return
  }
  if (r.name === 'chat' && routeTasks()) {
    // "#/g/<id>?tasks": open the group's task panel (link from the #/tasks board), then drop the flag.
    history.replaceState(history.state, '', `#/g/${encodeURIComponent(r.gid)}`)
    setState({ route: r, taskPanel: r.gid })
    return
  }
  const seq = r.name === 'chat' ? routeSeq() : null
  if (r.name === 'chat' && seq) {
    // Hand the seq to MessageList (same path as a notification click) and drop it from the URL,
    // so a reload or a later visit doesn't jump again.
    history.replaceState(history.state, '', `#/g/${encodeURIComponent(r.gid)}`)
    setState({ route: r, focus: { gid: r.gid, seq } })
    return
  }
  setState({ route: r })
}

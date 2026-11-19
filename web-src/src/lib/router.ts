import { getState, setState, type Route } from './store'

export const PUBLIC_ROUTES = new Set<Route['name']>(['login', 'redeem'])

export function parseRoute(hash = location.hash): Route {
  const p = hash.replace(/^#\/?/, '').split('/').map((x) => {
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
      return { name: 'agents' }
    case 'admin':
      return { name: 'admin' }
    case 'me':
      return { name: 'me' }
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
  setState({ route: r })
}

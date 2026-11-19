import { lazy, Suspense, useEffect, useState } from 'react'
import { Bot, MessagesSquare, Plus } from 'lucide-react'
import { Sidebar } from '@/components/pichat/sidebar'
import { RestartBanner } from '@/components/pichat/restart-banner'
import { Empty } from '@/components/pichat/page'
import { Button } from '@/components/ui/button'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { api, setUnauthorizedHandler } from '@/lib/api'
import { setFaviconBadge } from '@/lib/desktop-notify'
import { closeEvents, connectEvents, eventsAlive } from '@/lib/events'
import { parseRoute, syncRoute, takeAfterLogin } from '@/lib/router'
import { fail, getState, loadGroups, loadServerVersion, loadUpdateFlag, notify, resetState, setState, softSync, useStore } from '@/lib/store'
import type { Me } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Login, Redeem } from '@/pages/Auth'
import { Chat } from '@/pages/Chat'

const Account = lazy(() => import('@/pages/Account').then((m) => ({ default: m.Account })))
const Admin = lazy(() => import('@/pages/Admin').then((m) => ({ default: m.Admin })))
const Agents = lazy(() => import('@/pages/Agents').then((m) => ({ default: m.Agents })))
const GroupSettings = lazy(() => import('@/pages/GroupSettings').then((m) => ({ default: m.GroupSettings })))

function Main() {
  const route = useStore((s) => s.route)
  switch (route.name) {
    case 'chat':
      return <Chat key={route.gid} gid={route.gid} />
    case 'settings':
      return <GroupSettings key={route.gid} gid={route.gid} />
    case 'agents':
      return <Agents />
    case 'admin':
      return <Admin />
    case 'me':
      return <Account />
    default:
      return (
        <Empty
          icon={<MessagesSquare />}
          title="欢迎回来"
          actions={
            <>
              <Button onClick={() => window.dispatchEvent(new Event('pichat:new-group'))}>
                <Plus />
                新建群
              </Button>
              <Button variant="outline" asChild>
                <a href="#/agents">
                  <Bot />
                  添加 agent
                </a>
              </Button>
            </>
          }
        >
          从左侧选一个会话继续聊，<wbr />或者建个新群，把你的 agent 拉进来。
        </Empty>
      )
  }
}

function Shell() {
  const atHome = useStore((s) => s.route.name === 'home')
  return (
    <div className="flex h-dvh min-h-0 bg-background">
      {/* Below 900px the sidebar is the home screen and every other route is full screen. */}
      <div
        className={cn(
          'min-h-0 w-full min-[900px]:flex min-[900px]:w-[280px] min-[900px]:shrink-0 min-[900px]:border-r min-[900px]:border-sidebar-border',
          atHome ? 'flex' : 'hidden',
        )}
      >
        <Sidebar />
      </div>
      <main
        className={cn(
          'min-h-0 min-w-0 flex-1 flex-col bg-card min-[900px]:my-2 min-[900px]:mr-2 min-[900px]:rounded-card min-[900px]:shadow-card',
          atHome ? 'hidden min-[900px]:flex' : 'flex',
        )}
      >
        <Suspense fallback={null}>
          <Main />
        </Suspense>
      </main>
    </div>
  )
}

// The session cookie is HttpOnly; this marker lets a logged-out page load skip the /me probe (and its 401).
const SESSION_HINT = 'pichat:session'

function login(me: Me) {
  localStorage.setItem(SESSION_HINT, '1')
  setState({ me })
  loadGroups().catch(fail)
  connectEvents()
  loadServerVersion().catch(() => {})
  loadUpdateFlag()
}

export default function App() {
  const [boot, setBoot] = useState(true)
  const route = useStore((s) => s.route)
  const me = useStore((s) => s.me)
  const unread = useStore((s) => s.groups.reduce((n, g) => n + (g.unread || 0), 0))
  // mentionsMe includes @me, replies to me and DMs (server counts reasons mention/dm/reply).
  const hot = useStore((s) => s.groups.some((g) => g.mentionsMe > 0 || (g.kind === 'dm' && g.unread > 0)))

  useEffect(() => {
    const onLogin = async (e: Event) => {
      let user = (e as CustomEvent<Me>).detail
      try {
        user = await api<Me>('GET', '/me')
      } catch {
        /* use login payload */
      }
      login(user)
      if (parseRoute().name === 'login') location.hash = takeAfterLogin()
      else syncRoute()
    }
    const onLogout = () => {
      localStorage.removeItem(SESSION_HINT)
      closeEvents()
      resetState()
      location.hash = '#/login'
    }
    setUnauthorizedHandler(() => {
      localStorage.removeItem(SESSION_HINT)
      if (getState().me) {
        notify('登录已过期，请重新登录')
        onLogout()
      }
    })
    const onVisible = () => {
      if (document.visibilityState !== 'visible' || !getState().me) return
      if (!eventsAlive()) connectEvents()
      softSync()
    }

    window.addEventListener('hashchange', syncRoute)
    window.addEventListener('pichat:login', onLogin)
    window.addEventListener('pichat:logout', onLogout)
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('online', onVisible)

    const probe = localStorage.getItem(SESSION_HINT)
      ? api<Me & { kind: string }>('GET', '/me').then((m) => {
          if (m.kind === 'human') login(m)
        })
      : Promise.resolve()
    probe
      .catch(() => {})
      .finally(() => {
        setBoot(false)
        syncRoute()
      })

    return () => {
      window.removeEventListener('hashchange', syncRoute)
      window.removeEventListener('pichat:login', onLogin)
      window.removeEventListener('pichat:logout', onLogout)
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('online', onVisible)
    }
  }, [])

  useEffect(() => {
    document.title = unread ? `(${unread > 99 ? '99+' : unread}) pichat` : 'pichat'
  }, [unread])

  useEffect(() => setFaviconBadge(!!me && hot), [me, hot])

  let body
  if (boot) body = <Empty className="h-dvh">加载中…</Empty>
  else if (route.name === 'login') body = <Login />
  else if (route.name === 'redeem') body = <Redeem code={route.code} />
  else if (!me) body = <Empty className="h-dvh">加载中…</Empty>
  else body = <Shell />

  return (
    <TooltipProvider delayDuration={300}>
      {body}
      <RestartBanner />
      <Toaster position="top-center" />
    </TooltipProvider>
  )
}

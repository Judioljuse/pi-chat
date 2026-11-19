import { useEffect } from 'react'
import { Loader2 } from 'lucide-react'
import { getState, useStore } from '@/lib/store'

const POLL_MS = 1500
// Restart into the same version (e.g. the new binary failed and the old one came back): reload anyway.
const GIVE_UP_MS = 45_000

/** Full-width banner while the server restarts for an update; reloads the page once it is back. */
export function RestartBanner() {
  const restarting = useStore((s) => s.restarting)

  useEffect(() => {
    if (!restarting) return
    const before = getState().serverVersion
    const started = Date.now()
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const r = await fetch('/api/v1', { cache: 'no-store', credentials: 'same-origin' })
        if (r.ok) {
          const v = ((await r.json()) as { version?: string }).version || ''
          if ((v && v !== before) || Date.now() - started > GIVE_UP_MS) {
            location.reload()
            return
          }
        }
      } catch {
        /* server is down: keep waiting */
      }
      if (!stopped) timer = setTimeout(poll, POLL_MS)
    }
    timer = setTimeout(poll, POLL_MS)
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [restarting])

  if (!restarting) return null
  return (
    <div
      role="status"
      className="fixed inset-x-0 top-0 z-50 flex items-center justify-center gap-2 bg-brand px-4 py-2 text-[13px] text-primary-foreground shadow-raised"
    >
      <Loader2 className="size-4 animate-spin" />
      服务器正在更新…页面会在服务器恢复后自动刷新
    </div>
  )
}

import { BellRing, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { dismissNotifyBanner, enableDesktopNotify, useNotifyStatus } from '@/lib/desktop-notify'

/** Sidebar prompt to turn on desktop notifications. Permission is only requested from the button click. */
export function NotifyBanner() {
  const st = useNotifyStatus()
  // Insecure origins report 'denied' in Chrome: still show the HTTPS/localhost hint there.
  if (!st.supported || st.dismissed || st.permission === 'granted' || (st.secure && st.permission === 'denied')) return null
  return (
    <div className="mx-2 mb-2 flex items-start gap-2 rounded-card border border-border bg-card p-2.5 animate-fade-up">
      <BellRing className="mt-0.5 size-4 shrink-0 text-brand" />
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <span className="text-[12.5px] text-body">有人 @ 你、回复你或私聊你时，弹出桌面通知。</span>
        {st.secure ? (
          <Button size="xs" className="w-fit" onClick={() => enableDesktopNotify()}>
            开启桌面通知
          </Button>
        ) : (
          <span className="text-[12px] text-muted-foreground">桌面通知需要 HTTPS 或 localhost 访问（例如 tailscale serve 或 ssh -L）</span>
        )}
      </div>
      <button
        type="button"
        aria-label="关闭"
        onClick={dismissNotifyBanner}
        className="grid size-5 shrink-0 place-items-center rounded-chip text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </div>
  )
}

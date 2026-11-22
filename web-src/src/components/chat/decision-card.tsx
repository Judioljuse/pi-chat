import { useState } from 'react'
import { Check, LoaderCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { fmtTime } from '@/lib/format'
import { answerDecision, fail, useStore } from '@/lib/store'
import type { Decision } from '@/lib/types'
import { cn } from '@/lib/utils'

/**
 * A decision an agent asked (v0.4.0): option buttons for the person asked (or a group manager),
 * the answer once given. The question text itself is the message above it.
 */
export function DecisionCard({ d, canAnswer, className }: { d: Decision; canAnswer: boolean; className?: string }) {
  const [busy, setBusy] = useState<number | 'text' | null>(null)
  const [note, setNote] = useState('')
  const meId = useStore((s) => s.me?.id)
  const open = d.status === 'open'

  const pick = async (i: number | null) => {
    if (busy !== null) return
    if (i === null && !note.trim()) return
    setBusy(i ?? 'text')
    try {
      await answerDecision(d, i, note.trim())
      setNote('')
    } catch (e) {
      fail(e)
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className={cn('flex w-full max-w-md flex-col gap-2 rounded-[12px] border border-border bg-card p-3', className)}>
      <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
        <span
          className={cn(
            'rounded-chip px-1.5 py-px text-[11.5px] font-medium',
            open ? 'bg-brand/10 text-brand' : d.status === 'answered' ? 'bg-accent text-foreground' : 'bg-accent text-muted-foreground',
          )}
        >
          {open ? '待决定' : d.status === 'answered' ? '已决定' : '已取消'}
        </span>
        <span>
          问 {d.askTo.id === meId ? '你' : d.askTo.name} · {fmtTime(d.createdAt)}
        </span>
      </div>
      <div className="flex flex-col gap-1.5">
        {d.options.map((o, i) => {
          const chosen = d.status === 'answered' && !!d.answer && (d.answer === o || d.answer.startsWith(o + '。'))
          return (
            <Button
              key={i}
              type="button"
              variant={chosen ? 'default' : 'outline'}
              size="sm"
              disabled={!open || !canAnswer || busy !== null}
              onClick={() => pick(i)}
              className="h-auto min-h-8 justify-start py-1.5 text-left whitespace-normal"
            >
              {busy === i ? <LoaderCircle className="animate-spin" /> : chosen ? <Check /> : <span className="w-4 text-center tabular-nums opacity-60">{i + 1}</span>}
              <span className="min-w-0 flex-1">{o}</span>
            </Button>
          )
        })}
      </div>
      {open && canAnswer && (
        <form
          className="flex gap-1.5"
          onSubmit={(e) => {
            e.preventDefault()
            pick(null)
          }}
        >
          <Input
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="补充说明（可选）"
            className="h-8 text-[13px]"
            maxLength={1000}
          />
          <Button type="submit" size="sm" variant="ghost" disabled={!note.trim() || busy !== null}>
            {busy === 'text' ? <LoaderCircle className="animate-spin" /> : '回复'}
          </Button>
        </form>
      )}
      {d.status === 'answered' && d.answer && (
        <p className="text-[12.5px] text-body">
          回复：{d.answer}
          {d.answeredAt ? <span className="ml-1.5 text-muted-foreground tabular-nums">{fmtTime(d.answeredAt)}</span> : null}
        </p>
      )}
    </div>
  )
}

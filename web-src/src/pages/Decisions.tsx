import { useEffect, useMemo } from 'react'
import { ClipboardCheck } from 'lucide-react'
import { DecisionCard } from '@/components/chat/decision-card'
import { Empty, Page } from '@/components/pichat/page'
import { fmtTime } from '@/lib/format'
import { loadDecisions, setState, useStore } from '@/lib/store'
import type { Decision } from '@/lib/types'

/** #/decisions: open decisions agents asked of me, across all groups (v0.4.0). */
export function Decisions() {
  const list = useStore((s) => s.decisions)
  useEffect(() => {
    loadDecisions()
  }, [])
  const byGroup = useMemo(() => {
    const m = new Map<string, Decision[]>()
    for (const d of list ?? []) m.set(d.groupId, [...(m.get(d.groupId) ?? []), d])
    return [...m.values()]
  }, [list])

  return (
    <Page title="待决定" back="#/">
      {list === null ? null : list.length === 0 ? (
        <Empty icon={<ClipboardCheck />} title="没有等你决定的事">
          agent 需要你拍板时（用 ask_decision），会出现在这里，你点一个选项就会回复到群里并叫醒它。
        </Empty>
      ) : (
        <div className="flex flex-col gap-6">
          {byGroup.map((ds) => (
            <section key={ds[0].groupId} className="flex flex-col gap-3">
              <h2 className="text-[13px] font-medium text-muted-foreground">{ds[0].groupName || '私聊'}</h2>
              {ds.map((d) => (
                <div key={d.id} className="flex flex-col gap-2">
                  <div className="flex items-baseline gap-2 text-[13px]">
                    <span className="font-medium text-foreground">{d.agent.name}</span>
                    <span className="text-muted-foreground tabular-nums">{fmtTime(d.createdAt)}</span>
                    <a
                      href={`#/g/${d.groupId}?seq=${d.seq}`}
                      onClick={() => setState({ focus: { gid: d.groupId, seq: d.seq } })}
                      className="ml-auto text-[12px] text-muted-foreground hover:text-foreground hover:underline"
                    >
                      去群里看 #{d.seq}
                    </a>
                  </div>
                  <p className="text-[14px] whitespace-pre-wrap text-foreground">{d.question}</p>
                  <DecisionCard d={d} canAnswer />
                </div>
              ))}
            </section>
          ))}
        </div>
      )}
    </Page>
  )
}

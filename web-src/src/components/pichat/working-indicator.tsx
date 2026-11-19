import { motion, useReducedMotion } from 'motion/react'
import LoadingState from '@/components/beautiful/loading-state'
import { EASE_OUT } from '@/lib/ease'
import { PrincipalAvatar } from './avatar'

export interface WorkingAgent {
  id: string
  name: string
  platform?: string | null
  startedAt: number
}

/** Bottom-of-conversation row: avatar, pixel grid, "CC 正在处理…" and a live timer per running agent. */
export function WorkingIndicator({ agents, layoutKey }: { agents: WorkingAgent[]; /** Changes when rows above change, so the row slides to its new spot. */ layoutKey?: number }) {
  const reduce = useReducedMotion()
  if (!agents.length) return null
  return (
    <div className="flex flex-col gap-2" aria-live="polite">
      {agents.map((a) => (
        <motion.div
          key={a.id}
          layout={reduce ? false : 'position'}
          layoutDependency={layoutKey}
          initial={reduce ? false : { opacity: 0, y: 8, filter: 'blur(1px)' }}
          animate={{ opacity: 1, y: 0, filter: 'blur(0px)' }}
          transition={{ duration: 0.3, ease: EASE_OUT, layout: { duration: 0.3, ease: EASE_OUT } }}
          className="flex items-center gap-3"
        >
          <PrincipalAvatar id={a.id} name={a.name} kind="agent" platform={a.platform} size="md" />
          <LoadingState label={`${a.name} 正在处理…`} startedAt={a.startedAt} />
        </motion.div>
      ))}
    </div>
  )
}

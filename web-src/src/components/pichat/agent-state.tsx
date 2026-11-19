import type { AgentState } from '@/lib/types'
import { cn } from '@/lib/utils'

export const AGENT_STATE: Record<AgentState, string> = {
  offline: '离线',
  idle: '空闲',
  running: '运行中',
  attached: '在终端中',
}

const DOT: Record<AgentState, string> = {
  offline: 'bg-border',
  idle: 'bg-online',
  running: 'bg-brand motion-safe:animate-pulse',
  attached: 'bg-warning',
}

const TEXT: Record<AgentState, string> = {
  offline: 'text-muted-foreground',
  idle: 'text-muted-foreground',
  running: 'text-brand',
  attached: 'text-warning',
}

/** Small dot + label for an agent's listener state. */
export function AgentStateChip({ state, className }: { state: AgentState; className?: string }) {
  return (
    <span className={cn('inline-flex shrink-0 items-center gap-1 text-[12px]', TEXT[state], className)}>
      <span className={cn('size-1.5 rounded-full', DOT[state])} />
      {AGENT_STATE[state]}
    </span>
  )
}

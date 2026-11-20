import { SquareTerminal } from 'lucide-react'
import type { AgentState } from '@/lib/types'
import { cn } from '@/lib/utils'

// 'attached' is a mode, not a load state: the main session is open in a terminal, which takes the
// group's @s itself. It shows as 空闲 (or 运行中 while working) plus a separate 终端模式 tag.
export const AGENT_STATE: Record<AgentState, string> = {
  offline: '离线',
  idle: '空闲',
  running: '运行中',
  attached: '空闲',
}

export const TERMINAL_MODE = '终端模式'

const DOT: Record<AgentState, string> = {
  offline: 'bg-border',
  idle: 'bg-online',
  running: 'bg-brand motion-safe:animate-pulse',
  attached: 'bg-online',
}

const TEXT: Record<AgentState, string> = {
  offline: 'text-muted-foreground',
  idle: 'text-muted-foreground',
  running: 'text-brand',
  attached: 'text-muted-foreground',
}

/** Tooltip/label text: "空闲", "运行中 · 终端模式", ... */
export function agentStateText(state: AgentState, terminal = false) {
  return terminal && state !== 'offline' ? `${AGENT_STATE[state]} · ${TERMINAL_MODE}` : AGENT_STATE[state]
}

/** Small "终端模式" tag shown next to the state, not instead of it. */
export function TerminalModeTag({ className }: { className?: string }) {
  return (
    <span
      title="主会话开在终端里，群里的 @ 由终端处理；终端退出后由后台处理"
      className={cn('inline-flex h-4 shrink-0 items-center gap-0.5 rounded-[4px] bg-tag px-1 text-[10.5px] leading-none font-medium text-brand', className)}
    >
      <SquareTerminal className="size-2.5" />
      {TERMINAL_MODE}
    </span>
  )
}

/** Small dot + label for an agent's listener state, plus the 终端模式 tag when the terminal holds the session. */
export function AgentStateChip({ state, terminal, className }: { state: AgentState; terminal?: boolean; className?: string }) {
  return (
    <span className={cn('inline-flex shrink-0 items-center gap-1.5', className)}>
      <span className={cn('inline-flex items-center gap-1 text-[12px]', TEXT[state], className)}>
        <span className={cn('size-1.5 rounded-full', DOT[state])} />
        {AGENT_STATE[state]}
      </span>
      {terminal && state !== 'offline' && <TerminalModeTag />}
    </span>
  )
}

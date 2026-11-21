import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

/** Claude Code mark: orange starburst. */
export function ClaudeGlyph({ className, ...props }: ComponentProps<'svg'>) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className} {...props}>
      <g fill="currentColor">
        {Array.from({ length: 12 }, (_, i) => (
          <rect key={i} x="11.1" y="1.6" width="1.8" height="9" rx="0.9" transform={`rotate(${i * 30} 12 12)`} />
        ))}
      </g>
    </svg>
  )
}

const HUES = [
  'bg-[#e8e3d9] text-[#6b5d43] dark:bg-[#3a352b] dark:text-[#d9cdb2]',
  'bg-[#dfe7ec] text-[#4e6a7e] dark:bg-[#26313a] dark:text-[#9cb6c8]',
  'bg-[#e3e9df] text-[#56704a] dark:bg-[#2b3527] dark:text-[#b2cfa4]',
  'bg-[#ece0dc] text-[#81574a] dark:bg-[#3a2b26] dark:text-[#e0b4a6]',
  'bg-[#e6e1ee] text-[#65558a] dark:bg-[#302a3c] dark:text-[#c3b5e6]',
]

function hue(id: string) {
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0
  return HUES[h % HUES.length]
}

const SIZE = {
  xs: 'size-5 text-[10px] rounded-[6px]',
  sm: 'size-6 text-[11px] rounded-[7px]',
  md: 'size-8 text-[13px] rounded-[9px]',
  lg: 'size-10 text-[15px] rounded-[11px]',
}

export interface PrincipalAvatarProps {
  id: string
  name: string
  kind: 'human' | 'agent'
  platform?: string | null
  size?: keyof typeof SIZE
  /** Show a small pulsing brand dot while the agent is running. */
  working?: boolean
  /** Show an online dot (agents only). */
  online?: boolean
  className?: string
}

export function PrincipalAvatar({ id, name, kind, platform, size = 'md', working, online, className }: PrincipalAvatarProps) {
  let face
  if (kind === 'agent' && platform === 'claude_code') {
    face = (
      <span className={cn('grid place-items-center bg-[#f6e6dc] text-claude dark:bg-[#3b2a22]', SIZE[size])}>
        <ClaudeGlyph className="size-[62%]" />
      </span>
    )
  } else if (kind === 'agent' && platform === 'codex') {
    // Neutral terminal prompt (not the OpenAI mark) on a dark rounded square.
    face = (
      <span className={cn('grid place-items-center bg-[#1f2328] font-mono font-semibold text-[#e6edf3] dark:bg-[#e6edf3] dark:text-[#1f2328]', SIZE[size])}>
        <span className="-mt-px text-[0.92em] leading-none tracking-[-0.06em]">&gt;_</span>
      </span>
    )
  } else if (kind === 'agent' && platform === 'pi') {
    face = (
      <span className={cn('grid place-items-center bg-foreground font-serif text-background italic', SIZE[size])}>
        <span className="-mt-px text-[1.15em] leading-none">π</span>
      </span>
    )
  } else {
    const ch = Array.from(name.trim())[0]?.toUpperCase() ?? '?'
    face = (
      <span
        className={cn(
          'grid place-items-center font-medium',
          kind === 'agent' ? 'bg-agent-light text-agent' : hue(id),
          kind === 'human' ? 'rounded-full' : '',
          SIZE[size],
        )}
      >
        {ch}
      </span>
    )
  }
  return (
    <span className={cn('relative inline-flex shrink-0 select-none', className)} aria-hidden="true">
      {face}
      {working ? (
        <span className="absolute -right-0.5 -bottom-0.5 z-10 grid size-[7px] place-items-center">
          <span className="absolute inset-0 rounded-full bg-brand opacity-60 motion-safe:animate-ping" />
          <span className="relative size-[7px] rounded-full bg-brand ring-[1.5px] ring-background" />
        </span>
      ) : online ? (
        <span className="absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-online ring-2 ring-background" />
      ) : null}
    </span>
  )
}

export function AgentTag({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex h-4 items-center rounded-[4px] bg-agent-light px-1 text-[10.5px] leading-none font-medium text-agent',
        className,
      )}
    >
      agent
    </span>
  )
}

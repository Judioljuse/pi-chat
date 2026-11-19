import { Crown } from 'lucide-react'
import { cn } from '@/lib/utils'

const LABEL = { owner: '群主', admin: '管理员', member: '成员', user: '用户' } as const

export type Role = keyof typeof LABEL

/** One badge for every role label: tagged for owner/admin, plain muted text otherwise. */
export function RoleBadge({ role, className }: { role: Role; className?: string }) {
  const tagged = role === 'owner' || role === 'admin'
  return (
    <span
      className={cn(
        'inline-flex h-5 shrink-0 items-center gap-1 rounded-chip text-[11.5px] leading-none',
        tagged ? 'bg-tag px-1.5 text-brand' : 'text-muted-foreground',
        className,
      )}
    >
      {role === 'owner' && <Crown className="size-3" />}
      {LABEL[role]}
    </span>
  )
}

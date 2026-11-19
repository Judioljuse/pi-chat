import type { ReactNode } from 'react'
import { ChevronLeft } from 'lucide-react'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

/** Standard page for settings-like screens: thin header, centered column. */
export function Page({
  title,
  back,
  actions,
  children,
  className,
}: {
  title: ReactNode
  back?: string
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4">
        {back && (
          <a
            href={back}
            aria-label="返回"
            className="-ml-1.5 grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground min-[900px]:hidden"
          >
            <ChevronLeft className="size-4" />
          </a>
        )}
        <h1 className="min-w-0 flex-1 truncate text-[15px] font-medium text-foreground">{title}</h1>
        {actions}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className={cn('mx-auto flex w-full max-w-[760px] flex-col gap-4 px-4 py-6 animate-panel-in', className)}>
          {children}
        </div>
      </div>
    </div>
  )
}

export function Section({ title, description, children, className }: { title?: ReactNode; description?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn('rounded-card border border-border bg-card p-4', className)}>
      {title && <h2 className="text-[14px] font-medium text-foreground">{title}</h2>}
      {description && <p className="mt-1 text-[13px] text-muted-foreground">{description}</p>}
      <div className={cn(title || description ? 'mt-3' : '', 'flex flex-col gap-3')}>{children}</div>
    </section>
  )
}

export function Field({ label, hint, htmlFor, children }: { label: ReactNode; hint?: ReactNode; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={htmlFor} className="text-[13px] font-medium text-body">
        {label}
      </Label>
      {children}
      {hint && <p className="text-[12px] text-muted-foreground">{hint}</p>}
    </div>
  )
}

export function Empty({
  icon,
  title,
  actions,
  children,
  className,
}: {
  icon?: ReactNode
  title?: ReactNode
  actions?: ReactNode
  children?: ReactNode
  className?: string
}) {
  if (!icon && !title && !actions)
    return <div className={cn('grid flex-1 place-items-center p-8 text-center text-[13px] text-muted-foreground', className)}>{children}</div>
  return (
    <div className={cn('grid flex-1 place-items-center p-8', className)}>
      <div className="flex max-w-sm flex-col items-center text-center animate-panel-in">
        {icon && <div className="mb-4 grid size-12 place-items-center rounded-[10px] bg-tag text-brand [&_svg]:size-5">{icon}</div>}
        {title && <h2 className="font-serif text-[22px] leading-tight text-foreground">{title}</h2>}
        {children && <p className="mt-2 max-w-[22em] text-[13.5px] text-balance text-muted-foreground">{children}</p>}
        {actions && <div className="mt-5 flex flex-wrap justify-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}

import { Copy } from 'lucide-react'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group'
import { copy } from '@/lib/format'
import { cn } from '@/lib/utils'

/** Read-only value with a copy button (invite links, secrets). */
export function CopyField({ value, label = '复制', className }: { value: string; label?: string; className?: string }) {
  return (
    <InputGroup className={cn('bg-background', className)}>
      <InputGroupInput readOnly value={value} onFocus={(e) => e.currentTarget.select()} className="font-mono text-[12.5px]" />
      <InputGroupAddon align="inline-end">
        <InputGroupButton onClick={() => copy(value)} aria-label={label}>
          <Copy />
          {label}
        </InputGroupButton>
      </InputGroupAddon>
    </InputGroup>
  )
}

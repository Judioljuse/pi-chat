import { useEffect, useMemo, useState } from 'react'
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { startDm } from '@/lib/dm'
import { PLATFORM } from '@/lib/format'
import { agentStateOf, fail, terminalModeOf, useStore, workingAgentIds } from '@/lib/store'
import type { Contact } from '@/lib/types'
import { AgentStateChip } from './agent-state'
import { AgentTag, PrincipalAvatar } from './avatar'

/** Everyone the user may DM (GET /contacts), searchable; picking one opens the DM. */
export function NewDmDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const meId = useStore((s) => s.me?.id)
  const working = useStore((s) => s.working)
  const busyAgents = useMemo(() => workingAgentIds({ working }), [working])
  const [contacts, setContacts] = useState<Contact[] | null>(null)
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState('')

  useEffect(() => {
    if (!open) return
    setQ('')
    const ac = new AbortController()
    api<{ contacts: Contact[] }>('GET', '/contacts', undefined, { signal: ac.signal })
      .then((r) => setContacts((r.contacts ?? []).filter((c) => c.id !== meId)))
      .catch((e) => {
        if (ac.signal.aborted) return
        setContacts((c) => c ?? [])
        fail(e)
      })
    return () => ac.abort()
  }, [open, meId])

  const filtered = useMemo(() => {
    const k = q.trim().toLowerCase()
    const list = [...(contacts ?? [])].sort((a, b) => a.displayName.localeCompare(b.displayName, 'zh-CN'))
    if (!k) return list
    return list.filter((c) =>
      [c.displayName, c.name, c.ownerName, PLATFORM[c.platform ?? ''] || c.platform].some((v) => v?.toLowerCase().includes(k)),
    )
  }, [contacts, q])
  const humans = filtered.filter((c) => c.kind === 'human')
  const agents = filtered.filter((c) => c.kind === 'agent')

  const pick = async (c: Contact) => {
    if (busy) return
    setBusy(c.id)
    try {
      await startDm(c.id)
      onOpenChange(false)
    } catch (e) {
      fail(e)
    } finally {
      setBusy('')
    }
  }

  const row = (c: Contact) => {
    const isAgent = c.kind === 'agent'
    const state = isAgent ? agentStateOf(c, busyAgents.has(c.id)) : null
    const platform = isAgent ? PLATFORM[c.platform ?? ''] || c.platform : ''
    return (
      <CommandItem
        key={c.id}
        value={c.id}
        disabled={!!busy && busy !== c.id}
        onSelect={() => pick(c)}
        className="gap-2.5 rounded-control px-2 py-1.5 [&>svg:last-child]:hidden"
      >
        <PrincipalAvatar
          id={c.id}
          name={c.displayName}
          kind={c.kind}
          platform={c.platform}
          size="md"
          online={state === 'idle' || state === 'attached'}
          working={state === 'running'}
        />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="min-w-0 truncate text-[13.5px] font-medium text-foreground">{c.displayName}</span>
            {isAgent && <AgentTag className="shrink-0" />}
            {platform && <span className="shrink-0 text-[12px] text-muted-foreground">{platform}</span>}
          </span>
          {isAgent && c.ownerName && (
            <span className="truncate text-[12px] text-muted-foreground">
              owner：{c.ownerId === meId ? '我' : c.ownerName}
            </span>
          )}
        </span>
        {state && <AgentStateChip state={state} terminal={terminalModeOf(c)} className="shrink-0" />}
      </CommandItem>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-md">
        <DialogHeader className="gap-1 px-5 pt-5 pb-3">
          <DialogTitle className="font-serif text-[20px] font-normal text-foreground">发起私聊</DialogTitle>
          <DialogDescription className="text-[13px]">可以私聊自己的 agent、和你在同一个群里的人和 agent。</DialogDescription>
        </DialogHeader>
        <Command label="搜索名字" shouldFilter={false} className="rounded-none! bg-card p-0">
          <div className="px-4">
            <CommandInput value={q} onValueChange={setQ} placeholder="搜索名字" aria-label="搜索名字" autoFocus />
          </div>
          <CommandList className="max-h-[min(60dvh,420px)] px-2 pt-1 pb-3">
            {contacts == null ? (
              <div className="flex flex-col gap-2 p-2">
                {[0, 1, 2].map((i) => (
                  <div key={i} className="flex items-center gap-2.5">
                    <Skeleton className="size-8 rounded-[9px]" />
                    <Skeleton className="h-3 w-32" />
                  </div>
                ))}
              </div>
            ) : (
              <>
                <CommandEmpty className="py-8 text-[13px] text-muted-foreground">
                  {contacts.length ? '没有匹配的人' : '还没有可以私聊的人。先进一个群，或者接入你的 agent。'}
                </CommandEmpty>
                {humans.length > 0 && <CommandGroup heading="人类">{humans.map(row)}</CommandGroup>}
                {agents.length > 0 && <CommandGroup heading="agent">{agents.map(row)}</CommandGroup>}
              </>
            )}
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  )
}

import { memo, type ComponentProps, type ReactNode } from 'react'
import Markdown, { type Components, defaultUrlTransform } from 'react-markdown'
import remarkBreaks from 'remark-breaks'
import remarkGfm from 'remark-gfm'
import { cn } from './utils'

export type MentionKind = 'human' | 'agent' | 'me' | 'all'

export interface MentionTarget {
  nick: string
  kind: MentionKind
}

// Exact shapes only: a "../" segment would let a message make the browser GET any same-origin URL.
const LOCAL_SRC = /^\/(?:api\/v1\/files\/[\w-]+|static\/(?:assets\/)?[\w-]+(?:\.[\w-]+)*)$/

// ---- @mention remark plugin ----

interface MdNode {
  type: string
  value?: string
  children?: MdNode[]
  data?: { hName?: string; hProperties?: Record<string, string> }
}

const SKIP = new Set(['code', 'inlineCode', 'link', 'linkReference', 'html'])
const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

function mentionPlugin(targets: MentionTarget[]) {
  const byNick = new Map(targets.map((t) => [t.nick.toLowerCase(), t]))
  // Longest nick first so "Alice2" wins over "Alice".
  const nicks = [...byNick.keys()].sort((a, b) => b.length - a.length).map(escapeRe)
  // A nick ends at whitespace, punctuation or the end (spec 6.5 step 3).
  const re = nicks.length
    ? new RegExp(`(^|[^\\w@])@(${nicks.join('|')})(?=$|[\\s\\p{P}\\p{S}])`, 'giu')
    : null

  const split = (value: string): MdNode[] | null => {
    if (!re || !value.includes('@')) return null
    const out: MdNode[] = []
    let last = 0
    re.lastIndex = 0
    for (let m = re.exec(value); m; m = re.exec(value)) {
      const at = m.index + m[1].length
      const t = byNick.get(m[2].toLowerCase())
      if (!t) continue
      if (at > last) out.push({ type: 'text', value: value.slice(last, at) })
      out.push({
        type: 'mention',
        data: { hName: 'mention', hProperties: { kind: t.kind } },
        children: [{ type: 'text', value: '@' + value.slice(at + 1, at + 1 + m[2].length) }],
      })
      last = at + 1 + m[2].length
    }
    if (!out.length) return null
    if (last < value.length) out.push({ type: 'text', value: value.slice(last) })
    return out
  }

  const walk = (node: MdNode) => {
    if (!node.children || SKIP.has(node.type)) return
    const next: MdNode[] = []
    for (const child of node.children) {
      if (child.type === 'text' && child.value) {
        const parts = split(child.value)
        if (parts) {
          next.push(...parts)
          continue
        }
      }
      walk(child)
      next.push(child)
    }
    node.children = next
  }

  return () => (tree: MdNode) => walk(tree)
}

// ---- components ----

const PILL: Record<MentionKind, string> = {
  human: 'bg-brand-light text-brand',
  agent: 'bg-agent-light text-agent',
  me: 'bg-mention-me-light text-mention-me font-semibold',
  all: 'bg-mention-me-light text-mention-me font-semibold',
}

export function MentionPill({ kind, children }: { kind: MentionKind; children: ReactNode }) {
  return (
    <span className={cn('mx-px inline-block rounded-[5px] px-1 py-px text-[0.95em] leading-tight font-medium', PILL[kind])}>
      {children}
    </span>
  )
}

const ExternalLink = ({ href, children }: ComponentProps<'a'>) => (
  <a href={href} target="_blank" rel="noopener noreferrer nofollow">
    {children}
  </a>
)

const components = {
  a: ({ node: _node, ...p }) => <ExternalLink {...p} />,
  img: ({ src, alt }) => {
    const s = typeof src === 'string' ? src : ''
    if (LOCAL_SRC.test(s)) return <img src={s} alt={alt ?? ''} loading="lazy" />
    return <ExternalLink href={s}>[图片：{alt || s}]</ExternalLink>
  },
  mention: ({ kind, children }: { kind: MentionKind; children: ReactNode }) => <MentionPill kind={kind}>{children}</MentionPill>,
} as Components

export interface MarkdownTextProps {
  text: string
  mentions?: MentionTarget[]
  className?: string
}

/** Safe markdown: no rehype-raw, so raw HTML is escaped; external images become links. */
export const MarkdownText = memo(function MarkdownText({ text, mentions = [], className }: MarkdownTextProps) {
  return (
    <div className={cn('md', className)}>
      <Markdown
        remarkPlugins={[remarkGfm, remarkBreaks, mentionPlugin(mentions)]}
        components={components}
        urlTransform={defaultUrlTransform}
      >
        {text}
      </Markdown>
    </div>
  )
})

/** One-line plain text for reply quotes and previews. */
export function plainText(md: string, max = 80): string {
  const s = md
    .replace(/```[\s\S]*?```/g, ' [代码] ')
    .replace(/(?:^[ \t]*\|.*\|[ \t]*(?:\n|$))+/gm, ' [表格] ')
    .replace(/(?:^.*\|.*\n)?^[ \t]*\|?[ \t]*:?-{3,}:?[ \t]*(?:\|[ \t]*:?-{3,}:?[ \t]*)+\|?[ \t]*(?:\n.*\|.*)*/gm, ' [表格] ')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '[图片$1]')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}(#{1,6}|>|[-*+]|\d+\.)\s+/gm, '')
    .replace(/(\*\*|__|~~|\*|_)(.+?)\1/g, '$2')
    .replace(/<[^>]+>/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  return s.length > max ? s.slice(0, max) + '…' : s
}

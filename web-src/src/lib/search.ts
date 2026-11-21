/** Message search (GET /groups/{id}/search) helpers shared by the chat search panel. */

export interface SearchHit {
  seq: number
  author: string
  createdAt: number
  snippet: string
}

type RawAuthor = string | { id?: string; name?: string; nick?: string } | null | undefined

/** Accepts `author` as a name string or a principal object, so small server shape changes don't break the panel. */
export function normalizeHit(raw: Record<string, unknown>): SearchHit | null {
  const seq = Number(raw.seq)
  if (!Number.isInteger(seq) || seq <= 0) return null
  const a = raw.author as RawAuthor
  const author =
    typeof a === 'string' ? a : a ? a.name || a.nick || a.id || '' : typeof raw.authorName === 'string' ? raw.authorName : ''
  const createdAt = Number(raw.createdAt) || 0
  const snippet = typeof raw.snippet === 'string' ? raw.snippet : typeof raw.text === 'string' ? raw.text : ''
  return { seq, author: author || '系统', createdAt, snippet }
}

export type HighlightPart = { text: string; match: boolean }

/** Splits text into plain and matching parts (case-insensitive, every occurrence of the trimmed query). */
export function highlightParts(text: string, query: string): HighlightPart[] {
  const q = query.trim()
  if (!q || !text) return text ? [{ text, match: false }] : []
  const hay = text.toLowerCase()
  const needle = q.toLowerCase()
  // toLowerCase can change length for a few scripts; fall back to no highlight rather than mis-slicing.
  if (hay.length !== text.length) return [{ text, match: false }]
  const out: HighlightPart[] = []
  let i = 0
  for (;;) {
    const j = hay.indexOf(needle, i)
    if (j < 0) break
    if (j > i) out.push({ text: text.slice(i, j), match: false })
    out.push({ text: text.slice(j, j + needle.length), match: true })
    i = j + needle.length
  }
  if (i < text.length) out.push({ text: text.slice(i), match: false })
  return out
}

/** Query string for the REST search; empty values are left out. */
export function searchQuery(q: string, opts: { from?: string; limit?: number } = {}): string {
  const p = new URLSearchParams({ q: q.trim() })
  if (opts.from?.trim()) p.set('from', opts.from.trim())
  if (opts.limit) p.set('limit', String(opts.limit))
  return p.toString()
}

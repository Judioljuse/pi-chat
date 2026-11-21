// Message references in chat text: "#12" (this group), "「群名」#12" and "群名 群 #12" (another group the viewer is in).
// Pure and dependency-free so `node --test` can run src/lib/seq-refs.test.mjs against it directly.

export interface RefGroup {
  id: string
  name: string
}

export interface SeqRef {
  /** The matched text, e.g. "#12" or "「开发」#12". */
  text: string
  seq: number
  /** Group the reference points at: the current group for a bare "#12". */
  gid: string
}

export type SeqPart = string | SeqRef

// 1-9 digits without a leading zero.
const NUM = '[1-9]\\d{0,8}'
// A bare "#12" must not follow an ASCII word character or URL/path punctuation ("abc#12", "page/#12", "a=#1").
const BAD_BEFORE = /[A-Za-z0-9_#&/?=.:@\-+~%\\]/
// ...and must not run into a letter, digit or underscore ("#12a", "#12_x"). CJK after it is fine ("#12号").
const BAD_AFTER = /[A-Za-z0-9_]/
// A group name in "<群名> 群 #12" must not continue an ASCII word ("xA 群 #1" is not group "A").
const BAD_BEFORE_NAME = /[A-Za-z0-9_]/

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** Compile once per (groups, current group); returns a splitter for text nodes. */
export function seqRefSplitter(
  groups: RefGroup[],
  currentGid: string,
  /** Highest seq known per group; a ref above it stays plain text. Unknown (undefined) accepts any seq. */
  lastSeqOf?: (gid: string) => number | undefined,
): (text: string) => SeqPart[] | null {
  const listed = groups
    .filter((g) => g.name && g.name.trim())
    .map((g) => ({ id: g.id, name: g.name.trim() }))
    // Longest first so "开发组2" wins over "开发组".
    .sort((a, b) => b.name.length - a.name.length)
  const byName = new Map<string, string>()
  for (const g of listed) if (!byName.has(g.name)) byName.set(g.name, g.id)
  const names = [...byName.keys()].map(escapeRe)

  // Each alternative captures (name, seq) or just (seq); remember where its groups start.
  const alts: string[] = []
  const named: number[] = [] // index of the name group of each named alternative
  let next = 1
  const add = (src: string, hasName: boolean) => {
    alts.push(src)
    if (hasName) named.push(next)
    next += hasName ? 2 : 1
  }
  if (names.length) {
    const n = names.join('|')
    // 「群名」#12
    add(`「(${n})」\\s?#(${NUM})`, true)
    // 群名 群 #12, and 开发群 #12 when the name itself ends with 群.
    add(`(${n})[ \\t]?群[ \\t]?#(${NUM})`, true)
    const endsQun = [...byName.keys()].filter((x) => x.endsWith('群')).map(escapeRe)
    if (endsQun.length) add(`(${endsQun.join('|')})[ \\t]?#(${NUM})`, true)
  }
  const bare = next
  add(`#(${NUM})`, false)
  const re = new RegExp(alts.join('|'), 'gu')
  const inRange = (gid: string, seq: number) => {
    const max = lastSeqOf?.(gid)
    return max === undefined || seq <= max
  }

  return (text: string) => {
    if (!text.includes('#')) return null
    const out: SeqPart[] = []
    let last = 0
    re.lastIndex = 0
    for (let m = re.exec(text); m; m = re.exec(text)) {
      const start = m.index
      const end = start + m[0].length
      const before = start > 0 ? text[start - 1] : ''
      const after = text[end] ?? ''
      // "#1.5" / "版本 #2.0" are numbers, not refs; a sentence-ending "#3." still links.
      const badAfter = BAD_AFTER.test(after) || /^[.,:]\d/.test(text.slice(end, end + 2))
      let ref: SeqRef | null = null
      const at = named.find((i) => m[i] !== undefined)
      if (at !== undefined) {
        const name = m[at]
        const seq = Number(m[at + 1])
        const bracket = at === named[0]
        const gid = byName.get(name)
        if (!bracket && BAD_BEFORE_NAME.test(before)) {
          // "xA 群 #1" is not group "A": retry one character later.
          re.lastIndex = start + 1
          continue
        }
        if (gid && !badAfter && inRange(gid, seq)) ref = { text: m[0], seq, gid }
        if (!ref) {
          // A named ref that doesn't resolve stays plain text as a whole; its "#12" is not about this group.
          re.lastIndex = end
          continue
        }
      } else {
        const seq = Number(m[bare])
        // "「别的群」#12" or "别的群 #12" (a group I'm not in) must not turn into a ref to this group.
        const head = text.slice(Math.max(0, start - 6), start)
        const otherGroup = /[」”][ \t]?$/u.test(head) || (/群[ \t]?$/u.test(head) && !/(本|此|该|这个|我们|咱们)群[ \t]?$/u.test(head))
        if (!BAD_BEFORE.test(before) && !otherGroup && !badAfter && inRange(currentGid, seq))
          ref = { text: m[0], seq, gid: currentGid }
        if (!ref) {
          re.lastIndex = start + 1
          continue
        }
      }
      if (start > last) out.push(text.slice(last, start))
      out.push(ref)
      last = end
    }
    if (!out.length) return null
    if (last < text.length) out.push(text.slice(last))
    return out
  }
}

/** One-shot helper (tests, previews). */
export function splitSeqRefs(
  text: string,
  groups: RefGroup[],
  currentGid: string,
  lastSeqOf?: (gid: string) => number | undefined,
): SeqPart[] {
  return seqRefSplitter(groups, currentGid, lastSeqOf)(text) ?? [text]
}

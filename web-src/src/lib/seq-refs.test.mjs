// Run: node --test src/lib/seq-refs.test.mjs (Node 22.18+ strips the TypeScript types).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { splitSeqRefs } from './seq-refs.ts'

const G = [
  { id: 'g_cur', name: '当前群' },
  { id: 'g_dev', name: '开发' },
  { id: 'g_dev2', name: '开发组' },
  { id: 'g_ops', name: '运维群' },
  { id: 'g_en', name: 'Ops Team' },
]
const refs = (text, last) => splitSeqRefs(text, G, 'g_cur', last).filter((p) => typeof p !== 'string')
const one = (text, last) => refs(text, last).map((r) => `${r.gid}:${r.seq}:${r.text}`)

test('bare #seq in the current group', () => {
  assert.deepEqual(one('见 #12 和 #3。'), ['g_cur:12:#12', 'g_cur:3:#3'])
  assert.deepEqual(one('#7'), ['g_cur:7:#7'])
  assert.deepEqual(one('(#7)'), ['g_cur:7:#7'])
  assert.deepEqual(one('#12号消息'), ['g_cur:12:#12'])
  assert.deepEqual(one('见#12'), ['g_cur:12:#12'])
})

test('keeps surrounding text intact', () => {
  assert.deepEqual(splitSeqRefs('a #1 b', G, 'g_cur'), ['a ', { text: '#1', seq: 1, gid: 'g_cur' }, ' b'])
  assert.deepEqual(splitSeqRefs('no refs', G, 'g_cur'), ['no refs'])
})

test('not part of a word, URL fragment or longer token', () => {
  assert.deepEqual(one('abc#12'), [])
  assert.deepEqual(one('x_#12'), [])
  assert.deepEqual(one('page/#12'), [])
  assert.deepEqual(one('https://example.com/a#12'), [])
  assert.deepEqual(one('index.html#1'), [])
  assert.deepEqual(one('?a=#1'), [])
  assert.deepEqual(one('##12'), [])
  assert.deepEqual(one('#12a'), [])
  assert.deepEqual(one('#12_'), [])
  assert.deepEqual(one('#0'), [])
  assert.deepEqual(one('#012'), [])
  assert.deepEqual(one('#abc'), [])
  assert.deepEqual(one('#1234567890'), [])
  assert.deepEqual(one('a#1 #2'), ['g_cur:2:#2'])
})

test('above lastSeq stays plain text', () => {
  const last = (gid) => ({ g_cur: 10, g_dev: 5 })[gid]
  assert.deepEqual(one('#10 #11', last), ['g_cur:10:#10'])
  assert.deepEqual(one('「开发」#5 「开发」#6', last), ['g_dev:5:「开发」#5'])
  // Unknown lastSeq (group not loaded): accept.
  assert.deepEqual(one('「运维群」#999', last), ['g_ops:999:「运维群」#999'])
})

test('cross-group: 「群名」#n', () => {
  assert.deepEqual(one('看「开发」#12'), ['g_dev:12:「开发」#12'])
  assert.deepEqual(one('看「开发组」#3'), ['g_dev2:3:「开发组」#3'])
  assert.deepEqual(one('「开发」 #4'), ['g_dev:4:「开发」 #4'])
  assert.deepEqual(one('「Ops Team」#4'), ['g_en:4:「Ops Team」#4'])
  // Not my group: plain text, and its #n does not point at this group.
  assert.deepEqual(one('「别的」#12'), [])
})

test('cross-group: 群名 群 #n', () => {
  assert.deepEqual(one('开发 群 #8'), ['g_dev:8:开发 群 #8'])
  assert.deepEqual(one('开发群 #8'), ['g_dev:8:开发群 #8'])
  assert.deepEqual(one('开发组群#9'), ['g_dev2:9:开发组群#9'])
  // A name ending with 群 also works without the extra 群.
  assert.deepEqual(one('运维群 #2'), ['g_ops:2:运维群 #2'])
  assert.deepEqual(one('Ops Team 群 #5'), ['g_en:5:Ops Team 群 #5'])
  assert.deepEqual(one('xOps Team 群 #5'), [])
  // Unknown group name before 群: plain text.
  assert.deepEqual(one('别的群 #12'), [])
  // ...but "本群 #n" is this group.
  assert.deepEqual(one('本群 #12'), ['g_cur:12:#12'])
})

test('no groups: only bare refs', () => {
  assert.deepEqual(splitSeqRefs('「开发」#1 #2', [], 'g'), ['「开发」#1 ', { text: '#2', seq: 2, gid: 'g' }])
})

test('regex metacharacters in group names', () => {
  const g = [{ id: 'g_x', name: 'a+b (1)' }]
  assert.deepEqual(splitSeqRefs('「a+b (1)」#3', g, 'c').filter((p) => typeof p !== 'string'), [{ text: '「a+b (1)」#3', seq: 3, gid: 'g_x' }])
})

test('bare refs work when no group name ends with 群', () => {
  const g = [{ id: 'g_a', name: '开发' }, { id: 'g_b', name: '运维' }]
  const r = splitSeqRefs('参考 #3 和「运维」#1，运维 群 #2', g, 'g_a').filter((p) => typeof p !== 'string')
  assert.deepEqual(r.map((x) => `${x.gid}:${x.seq}`), ['g_a:3', 'g_b:1', 'g_b:2'])
})

test('closing quote before a bare ref, with or without a space: not this group', () => {
  assert.deepEqual(one('「别人」#3'), [])
  assert.deepEqual(one('「别人」 #3'), [])
  assert.deepEqual(one('“别人” #3'), [])
  // A known group with the space still resolves to that group.
  assert.deepEqual(one('「开发」 #3'), ['g_dev:3:「开发」 #3'])
  // An opening quote right before is fine.
  assert.deepEqual(one('“#3”'), ['g_cur:3:#3'])
})

test('decimal / version numbers are not refs', () => {
  assert.deepEqual(one('#1.5'), [])
  assert.deepEqual(one('版本 #2.0'), [])
  assert.deepEqual(one('「开发」#2.0'), [])
  assert.deepEqual(one('#1,000'), [])
  // Sentence punctuation after a ref still links.
  assert.deepEqual(one('见 #3.'), ['g_cur:3:#3'])
  assert.deepEqual(one('#3, #4'), ['g_cur:3:#3', 'g_cur:4:#4'])
})

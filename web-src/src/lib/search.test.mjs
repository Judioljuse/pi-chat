// Run: node --test src/lib/search.test.mjs (Node 22.18+ strips the TypeScript types).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { highlightParts, normalizeHit, searchQuery } from './search.ts'

const marks = (t, q) => highlightParts(t, q).map((p) => (p.match ? `[${p.text}]` : p.text)).join('')

test('highlights every occurrence, case-insensitive', () => {
  assert.equal(marks('Deploy 部署 deploy', 'deploy'), '[Deploy] 部署 [deploy]')
  assert.equal(marks('今天部署了，明天再部署', '部署'), '今天[部署]了，明天再[部署]')
  assert.equal(marks('abc', 'x'), 'abc')
  assert.equal(marks('abc', '  '), 'abc')
  assert.deepEqual(highlightParts('', 'a'), [])
})

test('normalizes author shapes', () => {
  assert.deepEqual(normalizeHit({ seq: 3, author: { id: 'u1', name: '朱' }, createdAt: 5, snippet: 's' }), {
    seq: 3,
    author: '朱',
    createdAt: 5,
    snippet: 's',
  })
  assert.equal(normalizeHit({ seq: 3, author: 'CC', snippet: '' }).author, 'CC')
  assert.equal(normalizeHit({ seq: 3 }).author, '系统')
  assert.equal(normalizeHit({ seq: 0 }), null)
  assert.equal(normalizeHit({}), null)
})

test('builds the query string', () => {
  assert.equal(searchQuery(' 部署 '), 'q=%E9%83%A8%E7%BD%B2')
  assert.equal(searchQuery('a', { from: ' bob ', limit: 50 }), 'q=a&from=bob&limit=50')
})

// Run: node --test src/lib/group-limits.test.mjs (Node 22.18+ strips the TypeScript types).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { hopInputValue, parseLimits, rulesLength, sameLimits } from './group-limits.ts'

const f = (o = {}) => ({ hop: '6', unlimited: false, wakes: '30', perAgent: '60', ...o })

test('parses a normal form', () => {
  assert.deepEqual(parseLimits(f()), { body: { hopLimit: 6, agentWakesPerHour: 30, agentWakesPerAgentHour: 60 } })
})

test('unlimited hops sends -1 and ignores the hop input', () => {
  assert.deepEqual(parseLimits(f({ unlimited: true, hop: 'abc' })).body.hopLimit, -1)
})

test('rejects out-of-range and non-integer values', () => {
  for (const o of [{ hop: '0' }, { hop: '51' }, { hop: '-1' }, { hop: '2.5' }, { hop: '' }, { wakes: '0' }, { wakes: '1001' }, { perAgent: '0' }, { perAgent: '1001' }, { perAgent: 'x' }])
    assert.ok('error' in parseLimits(f(o)), JSON.stringify(o))
  assert.ok('body' in parseLimits(f({ hop: '50', wakes: '1000', perAgent: '1' })))
})

test('hop input falls back to the default when unlimited', () => {
  assert.equal(hopInputValue(-1), '6')
  assert.equal(hopInputValue(12), '12')
})

test('sameLimits and rulesLength', () => {
  const a = { hopLimit: 6, agentWakesPerHour: 30, agentWakesPerAgentHour: 60 }
  assert.ok(sameLimits(a, { ...a }))
  assert.ok(!sameLimits(a, { ...a, agentWakesPerAgentHour: 61 }))
  assert.equal(rulesLength('协作👍'), 3)
})

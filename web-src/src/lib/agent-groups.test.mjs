// Run: node --test src/lib/agent-groups.test.mjs
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { groupAgents, projectKey, projectLabel } from './agent-groups.ts'

const ag = (id, host, repo, online = false) => ({ id, displayName: id, online, runtime: host === undefined ? null : { host, dirs: repo === undefined ? null : { repo } } })

test('labels and keys', () => {
  assert.equal(projectLabel({ root: '/w/pi-chat', branch: 'main', worktree: false }), 'pi-chat · main')
  assert.equal(projectLabel({ root: '/w/pi-chat', branch: 'feat', worktree: true, path: '/w/pi-chat-feat' }), 'pi-chat · worktree feat')
  assert.equal(projectLabel({ root: 'C:\\w\\app\\', branch: null, worktree: false }), 'app')
  assert.equal(projectKey('mac', { root: '/w/a', branch: 'main', worktree: false }), 'proj:mac|/w/a|main')
  assert.equal(projectKey('mac', { root: '/w/a', branch: 'x', worktree: true, path: '/w/a-x' }), 'proj:mac|/w/a|/w/a-x')
})

test('groups by machine then project', () => {
  const main = { root: '/w/pi-chat', branch: 'main', worktree: false }
  const wt = { root: '/w/pi-chat', branch: 'feat', worktree: true, path: '/w/pi-chat-feat' }
  const g = groupAgents([
    ag('a', 'mac', main, true),
    ag('b', 'mac', wt),
    ag('c', 'mac', main),
    ag('d', 'mac', null, true),
    ag('e', 'linux', main),
    ag('f', undefined),
    ag('g', ''),
  ])
  assert.deepEqual(g.map((m) => [m.label, m.agents.length, m.online]), [['linux', 1, 0], ['mac', 4, 2], ['未知电脑', 2, 0]])
  const mac = g[1]
  assert.deepEqual(mac.projects.map((p) => [p.label, p.agents.map((a) => a.id)]), [
    ['pi-chat · main', ['a', 'c']],
    ['pi-chat · worktree feat', ['b']],
    ['其他目录', ['d']],
  ])
  assert.equal(mac.projects[2].key, null)
  assert.equal(g[2].key, 'host:?')
})

test('custom names override labels', () => {
  const main = { root: '/w/x', branch: 'main', worktree: false }
  const g = groupAgents([ag('a', 'mac', main)], { 'host:mac': '我的笔记本', 'proj:mac|/w/x|main': 'X 主干' })
  assert.equal(g[0].label, '我的笔记本')
  assert.equal(g[0].defaultLabel, 'mac')
  assert.equal(g[0].projects[0].label, 'X 主干')
  assert.equal(g[0].projects[0].defaultLabel, 'x · main')
})

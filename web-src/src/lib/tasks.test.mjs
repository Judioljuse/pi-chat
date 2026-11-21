// Run: node --test src/lib/tasks.test.mjs (Node 22.18+ strips the TypeScript types).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { boardQuery, matchesBoard, normalizeTask, normalizeTasks, openCount, removeTask, sectionTasks, sortTasks, taskTitleFrom, upsertTask } from './tasks.ts'

const t = (id, status, assignee, updatedAt = 1, extra = {}) =>
  normalizeTask({ id, groupId: 'g1', title: id, status, assignee: assignee ? { id: assignee, name: assignee.toUpperCase(), kind: 'human' } : null, updatedAt, ...extra })

test('normalizes the API shape and tolerates missing fields', () => {
  const x = normalizeTask({
    id: 't_1',
    groupId: 'g',
    groupName: '开发',
    title: '修 bug',
    note: null,
    status: 'doing',
    assignee: { id: 'a1', name: 'CC', kind: 'agent' },
    sourceSeq: 12,
    createdBy: { id: 'u1', name: '朱' },
    updatedBy: null,
    createdAt: 5,
    updatedAt: 7,
    doneAt: null,
  })
  assert.equal(x.note, '')
  assert.deepEqual(x.assignee, { id: 'a1', name: 'CC', kind: 'agent' })
  assert.equal(x.sourceSeq, 12)
  assert.equal(x.updatedBy, null)
  assert.equal(x.doneAt, null)
  assert.equal(normalizeTask({ title: 'no id' }), null)
  assert.equal(normalizeTask({ id: 'x', status: 'weird' }).status, 'todo')
  assert.equal(normalizeTask({ id: 'x', sourceSeq: 0 }).sourceSeq, null)
  assert.equal(normalizeTask({ id: 'x', createdAt: 3 }).updatedAt, 3)
  assert.deepEqual(normalizeTasks(null), [])
  assert.equal(normalizeTasks([{ id: 'a' }, null, {}]).length, 1)
})

test('sorts doing, blocked, todo, done; newest first within a status', () => {
  const list = [t('a', 'done', 'u'), t('b', 'todo', 'u', 1), t('c', 'todo', 'u', 9), t('d', 'blocked', 'u'), t('e', 'doing', 'u')]
  assert.deepEqual(sortTasks(list).map((x) => x.id), ['e', 'd', 'c', 'b', 'a'])
})

test('sections: me first, then by name, unassigned last; done split off', () => {
  const list = [t('1', 'todo', null), t('2', 'doing', 'zed'), t('3', 'done', 'me'), t('4', 'todo', 'amy'), t('5', 'blocked', 'me')]
  const s = sectionTasks(list, 'me', (id) => (id === 'amy' ? '阿美' : undefined))
  assert.deepEqual(s.map((x) => x.key), ['me', 'amy', 'zed', ''])
  assert.deepEqual(s[0].open.map((x) => x.id), ['5'])
  assert.deepEqual(s[0].done.map((x) => x.id), ['3'])
  assert.equal(s[1].assignee.name, '阿美')
  assert.equal(s[3].assignee, null)
})

test('upsert, remove and open count', () => {
  let list = [t('a', 'todo', 'u')]
  list = upsertTask(list, t('b', 'doing', 'u'))
  list = upsertTask(list, t('a', 'done', 'u'))
  assert.deepEqual(list.map((x) => `${x.id}:${x.status}`), ['a:done', 'b:doing'])
  assert.equal(openCount(list), 1)
  assert.equal(openCount(undefined), 0)
  assert.deepEqual(removeTask(list, 'a').map((x) => x.id), ['b'])
})

test('title from a message: whitespace collapsed, 80 characters', () => {
  assert.equal(taskTitleFrom('  部署\n  到  生产 '), '部署 到 生产')
  assert.equal(Array.from(taskTitleFrom('字'.repeat(100))).length, 80)
  assert.equal(taskTitleFrom('😀'.repeat(90)).length, 160)
})

test('board query and live filter', () => {
  assert.equal(boardQuery({ assignee: 'me', status: 'open' }), 'assignee=me&status=open')
  assert.equal(boardQuery({}), 'status=open')
  assert.equal(matchesBoard(t('a', 'done', 'me'), { assignee: 'me', status: 'open' }, 'me'), false)
  assert.equal(matchesBoard(t('a', 'done', 'me'), { assignee: 'me', status: 'all' }, 'me'), true)
  assert.equal(matchesBoard(t('a', 'todo', 'u2'), { assignee: 'me' }, 'me'), false)
  assert.equal(matchesBoard(t('a', 'todo', 'u2'), { assignee: 'u2' }, 'me'), true)
  assert.equal(matchesBoard(t('a', 'todo', null), {}, 'me'), true)
})

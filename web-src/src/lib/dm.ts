import { api } from './api'
import { navigate } from './router'
import { loadGroups } from './store'
import type { Group } from './types'

/** Open (or create) the DM with a principal and go there. POST /chats is idempotent per pair. */
export async function startDm(pid: string) {
  const { group } = await api<{ group: Group }>('POST', '/chats', { with: pid })
  loadGroups().catch(() => {})
  navigate(`#/g/${group.id}`)
  return group
}

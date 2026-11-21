/** Group relay limits and collaboration rules: form parsing shared by the group settings page. */

export const HOP_DEFAULT = 6
export const AGENT_WAKES_PER_AGENT_DEFAULT = 60
export const RULES_MAX = 2000

export interface LimitsForm {
  hop: string
  unlimited: boolean
  wakes: string
  perAgent: string
}

export interface LimitsBody {
  hopLimit: number
  agentWakesPerHour: number
  agentWakesPerAgentHour: number
}

const int = (s: string) => {
  const t = s.trim()
  return /^\d+$/.test(t) ? Number(t) : NaN
}

/** Turns the form into a PATCH body, or an error message (Chinese) when a field is out of range. */
export function parseLimits(f: LimitsForm): { body: LimitsBody } | { error: string } {
  const h = f.unlimited ? -1 : int(f.hop)
  const w = int(f.wakes)
  const a = int(f.perAgent)
  if (!f.unlimited && !(h >= 1 && h <= 50)) return { error: '接力上限要在 1～50 跳之间，或者勾选不限制' }
  if (!(w >= 1 && w <= 1000)) return { error: '每小时唤醒次数要在 1～1000 之间' }
  if (!(a >= 1 && a <= 1000)) return { error: '每个 agent 每小时被唤醒上限要在 1～1000 之间' }
  return { body: { hopLimit: h, agentWakesPerHour: w, agentWakesPerAgentHour: a } }
}

/** Number shown in the hop input: an unlimited group (-1) shows the default so unticking gives a sane value. */
export function hopInputValue(hopLimit: number): string {
  return String(hopLimit >= 1 ? hopLimit : HOP_DEFAULT)
}

export function sameLimits(a: LimitsBody, b: LimitsBody): boolean {
  return a.hopLimit === b.hopLimit && a.agentWakesPerHour === b.agentWakesPerHour && a.agentWakesPerAgentHour === b.agentWakesPerAgentHour
}

/** Rules length as the server counts it (Unicode code points). */
export function rulesLength(s: string): number {
  return [...s].length
}

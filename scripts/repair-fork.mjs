#!/usr/bin/env node
// One-off repair for listeners from before fork-safe resume: records the background branch's
// leaf (main.leaf) in the listener's state.json, so the next run resumes there with
// --resume-session-at instead of following a terminal window's newer branch.
//
//   node scripts/repair-fork.mjs --name <listener name> [--home ~/.pichat] [--claude-dir ~/.claude] [--dry-run]
//
// Background leaf = the last assistant row written by a headless run (entrypoint sdk-cli) on the
// branch that holds the most recent headless prompt. Claude agents only. state.json is backed up
// next to itself first. It refuses to write while the listener is in a run or a terminal is
// attached (the run's own result would be overwritten); a listener idling in the background
// adopts the new record before its next run.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const args = process.argv.slice(2);
const opt = (k) => { const i = args.indexOf(k); return i >= 0 && args[i + 1] && !args[i + 1].startsWith('--') ? args[i + 1] : undefined; };
const dry = args.includes('--dry-run');
const name = opt('--name');
const tilde = (p) => p.replace(/^~(?=\/|$)/, os.homedir());
if (!name || !/^[A-Za-z0-9_.-]{1,40}$/.test(name)) { console.error('用法: repair-fork.mjs --name <名字> [--home ~/.pichat] [--claude-dir ~/.claude] [--dry-run]'); process.exit(2); }
const home = path.resolve(tilde(opt('--home') || process.env.PICHAT_HOME || path.join(os.homedir(), '.pichat')));
const claudeDir = path.resolve(tilde(opt('--claude-dir') || process.env.CLAUDE_CONFIG_DIR || path.join(os.homedir(), '.claude')));
const dir = path.join(home, 'agents', name);
const stFile = path.join(dir, 'state.json');

const die = (m) => { console.error(m); process.exit(1); };
let st;
try { st = JSON.parse(fs.readFileSync(stFile, 'utf8')); } catch (e) { die(`读不了 ${stFile}：${e.message}`); }
const cfg = (() => { try { return JSON.parse(fs.readFileSync(path.join(dir, 'config.json'), 'utf8')); } catch { return {}; } })();
const main = st.main;
if (!main?.id) die('state.json 里没有主会话（main），不需要修复');
if ((main.agent || cfg.agent) !== 'claude') die(`主会话是 ${main.agent || cfg.agent} 的，这个脚本只处理 claude`);

// <claude dir>/projects/*/<id>.jsonl, most recently written.
let file = '', mtime = -1;
const projects = path.join(claudeDir, 'projects');
for (const d of fs.readdirSync(projects, { withFileTypes: true })) {
  if (!d.isDirectory()) continue;
  const f = path.join(projects, d.name, `${main.id}.jsonl`);
  const s = fs.statSync(f, { throwIfNoEntry: false });
  if (s?.isFile() && s.mtimeMs > mtime) { file = f; mtime = s.mtimeMs; }
}
if (!file) die(`找不到会话文件 ${projects}/*/${main.id}.jsonl`);

const rows = [];
for (const line of fs.readFileSync(file, 'utf8').split('\n')) {
  if (!line.trim()) continue;
  try { const o = JSON.parse(line); if (typeof o.uuid === 'string' && !o.isSidechain) rows.push(o); } catch {}
}
const byId = new Map(rows.map((r) => [r.uuid, r]));
const parentOf = (id) => { const r = byId.get(id); return r ? r.parentUuid || r.logicalParentUuid || '' : ''; };
const ancestors = (id) => { const out = []; const seen = new Set(); while (id && !seen.has(id) && byId.has(id)) { seen.add(id); out.push(id); id = parentOf(id); } return out; };
const isPrompt = (r) => r.type === 'user' && r.message?.role === 'user' && (typeof r.message.content === 'string'
  || (Array.isArray(r.message.content) && r.message.content.some((c) => c?.type === 'text') && !r.message.content.some((c) => c?.type === 'tool_result')));
const short = (r) => {
  const c = r.message?.content;
  const t = typeof c === 'string' ? c : Array.isArray(c) ? c.filter((x) => x?.type === 'text').map((x) => x.text).join(' ') : '';
  return t.replace(/\s+/g, ' ').slice(0, 40);
};

const head = [...rows].reverse().find((r) => ['user', 'assistant', 'system'].includes(r.type))?.uuid;
const lastBgPrompt = [...rows].reverse().find((r) => r.entrypoint === 'sdk-cli' && isPrompt(r));
if (!lastBgPrompt) die('会话里没有后台（sdk-cli）写入的对话，不需要修复');
// Last headless assistant row that descends from that prompt.
const leafRow = [...rows].reverse().find((r) => r.type === 'assistant' && r.entrypoint === 'sdk-cli' && ancestors(r.uuid).includes(lastBgPrompt.uuid));
if (!leafRow) die(`最近的后台 prompt ${lastBgPrompt.uuid} 之后没有后台 assistant 回复`);
const leaf = leafRow.uuid;

const headChain = ancestors(head);
const leafChain = ancestors(leaf);
const forked = !headChain.includes(leaf);
console.log(`listener：${name}（${dir}）`);
console.log(`主会话：${main.id}`);
console.log(`会话文件：${file}（${rows.length} 行对话记录）`);
console.log(`最近的后台 prompt：${lastBgPrompt.uuid} ${lastBgPrompt.timestamp || ''} "${short(lastBgPrompt)}"`);
console.log(`后台 leaf：${leaf} ${leafRow.timestamp || ''}`);
console.log(`文件最新位置（普通 --resume 会接着它）：${head} ${byId.get(head)?.timestamp || ''} entrypoint=${byId.get(head)?.entrypoint || '?'}`);
if (forked) {
  const onLeaf = new Set(leafChain);
  const common = headChain.find((id) => onLeaf.has(id));
  const after = (c) => (common ? c.slice(0, c.indexOf(common)) : c);
  const n = (c) => after(c).filter((id) => isPrompt(byId.get(id))).length;
  console.log(`分叉：是。分叉点 ${common || '（无共同祖先）'} ${byId.get(common)?.timestamp || ''}；后台分支 ${n(leafChain)} 轮，终端分支 ${n(headChain)} 轮`);
} else console.log('分叉：否（后台 leaf 在最新分支上，普通 --resume 不会丢内容）');
console.log(`state.json 当前 main.leaf：${main.leaf || '（无）'}`);

if (main.leaf === leaf) { console.log('main.leaf 已经是这个值，不需要修改'); process.exit(0); }
if (!forked) { console.log('没有分叉，不需要修改（没有 main.leaf 时 listener 用普通 --resume，结果相同）'); process.exit(0); }
// Same liveness test as the listener: pid alive and, on Linux, the same process start time.
const procStart = (pid) => { try { const st = fs.readFileSync(`/proc/${pid}/stat`, 'utf8'); return st.slice(st.lastIndexOf(')') + 2).split(' ')[19] || ''; } catch { return ''; } };
const alive = (pid, start) => {
  if (!Number.isInteger(pid) || pid <= 0) return false;
  try { process.kill(pid, 0); } catch (e) { if (e.code !== 'EPERM') return false; }
  if (start && process.platform === 'linux') { const now = procStart(pid); if (now && now !== start) return false; }
  return true;
};
const busy = ['running.json', 'attach.lock'].map((n) => {
  let l = null; try { l = JSON.parse(fs.readFileSync(path.join(dir, n), 'utf8')); } catch {}
  return l && (alive(l.pid, l.start) || alive(l.childPid, l.childStart)) ? `${n}（pid ${l.childPid && alive(l.childPid, l.childStart) ? l.childPid : l.pid}）` : '';
}).filter(Boolean);
const backup = `${stFile}.pre-repair-${new Date().toISOString().replace(/[:.]/g, '-')}`;
if (dry) {
  console.log(`--dry-run：会把 ${stFile} 备份到 ${backup}，然后写入 main.leaf=${leaf}（并更新 main.setAt，空闲中的 listener 会在下一次 run 前采用）`);
  if (busy.length) console.log(`注意：现在 ${busy.join('、')} 正在进行，真正写入时会拒绝，等它结束再运行`);
  process.exit(0);
}
if (busy.length) die(`listener 正在 run 或终端正在 attach（${busy.join('、')}），现在写入会覆盖它的结果。等它结束后再运行。`);
fs.copyFileSync(stFile, backup);
st.main = { ...main, leaf, setAt: Date.now() };
const tmp = `${stFile}.${process.pid}.tmp`;
fs.writeFileSync(tmp, JSON.stringify(st, null, 2) + '\n', { mode: 0o600 });
fs.renameSync(tmp, stFile);
console.log(`已备份到 ${backup}，写入 main.leaf=${leaf}`);

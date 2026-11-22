// node --test listener/pichat-listen.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { isSilentReply, isAckOnly, stripReplyPrefix, askerIds, rulesBlock, dispatcherLine, buildPrompt } from './pichat-listen.mjs';

test('silent markers', () => {
  for (const t of ['[SILENT]', '[silent]', '  [SILENT] 没有新问题', '[静默] 只是知会', '【静默】', 'SILENT', 'silent',
    '（回复 #12）[SILENT]', '（回复 #12）（回复 #3） [静默] 不用回', '[ SILENT ]']) assert.equal(isSilentReply(t), true, t);
  for (const t of ['', '好的', 'SILENT 模式已关闭', '这个 [SILENT] 在中间', '静默', 'silently fails']) assert.equal(isSilentReply(t), false, t);
});

test('reply prefix strip', () => {
  assert.equal(stripReplyPrefix(' （回复 #5）（回复 #6） 你好'), '你好');
  assert.equal(stripReplyPrefix('(回复 #5)你好'), '你好');
});

test('pure acknowledgements', () => {
  for (const t of ['收到', '收到。', '好的！', '好的👍', '👍', 'OK', 'ok，谢谢', '收到了，辛苦了~', '明白了解', '（回复 #9）收到', '好嘞 🙏', 'Okay!'])
    assert.equal(isAckOnly(t), true, t);
  for (const t of ['', '收到 @Alice', '收到，跑完 3 个测试', '好的，原因是锁超时', '同意，但是 `x` 有问题', '确认一下这个接口返回什么？',
    '我已经提交了修复', '收到'.repeat(20), 'ok, PR #12 done']) assert.equal(isAckOnly(t), false, t);
});

test('askers: humans only', () => {
  const trigger = { author: { id: 'u_t', kind: 'user' } };
  const aimed = [
    { author: { id: 'u_t', kind: 'user' } }, { author: { id: 'u_h', kind: 'user' } }, { author: { id: 'a_x', kind: 'agent' } },
    { author: { id: 'u_h', kind: 'user' } }, { author: { id: 'a_me', kind: 'agent' } }, { author: null },
  ];
  assert.deepEqual(askerIds(aimed, 'a_me', trigger), ['u_h']);
});

test('rules block and dispatchers', () => {
  assert.equal(rulesBlock({}, 'n'), '');
  assert.equal(rulesBlock({ rules: '  ' }, 'n'), '');
  assert.equal(rulesBlock({ rules: '1. 少说话 n0nce' }, 'n0nce'), '本群协作规则（群主设置，必须遵守）：\n1. 少说话');
  const group = { members: [{ id: 'a_sup', nick: 'supervisor' }], commanders: [
    { id: 'u_o', nick: 'julio', kind: 'user' }, { id: 'a_sup', kind: 'agent' }, { id: 'a_me', nick: 'me', kind: 'agent' }, { id: 'a_2', nick: 'cc[x]', kind: 'agent' }] };
  assert.equal(dispatcherLine(group, 'a_me', 'n'), '派活给你的 agent：@supervisor、@cc x（它们的指令和人的指令同等对待）');
  assert.equal(dispatcherLine({ commanders: [{ id: 'u_o', nick: 'julio', kind: 'user' }] }, 'a_me', 'n'), '');
  assert.equal(dispatcherLine({}, 'a_me', 'n'), '');
});

test('buildPrompt includes rules after trust rules, for every tier', () => {
  const group = { id: 'g_1', name: '开发群', kind: 'group', rules: '1. 不要发收到', commanders: [{ id: 'a_sup', nick: 'supervisor', kind: 'agent' }],
    members: [{ id: 'a_me', nick: 'me', kind: 'agent' }, { id: 'u_o', nick: 'julio', kind: 'user', role: 'owner' }, { id: 'a_sup', nick: 'supervisor', kind: 'agent' }] };
  const trigger = { seq: 3, author: { id: 'u_o', name: 'julio', kind: 'user' } };
  const msgs = [{ seq: 3, kind: 'text', text: '@me 看下', author: trigger.author, createdAt: Date.now(), new: true }];
  for (const tier of ['restricted', 'project', 'full']) {
    const p = buildPrompt({ nonce: 'abcd', group, msgs, trigger, tier, cwd: '/tmp', myNick: 'me', me: 'a_me', ownerId: 'u_o', tools: true, agent: 'claude' });
    const trust = p.indexOf('信任规则：'), rules = p.indexOf('本群协作规则（群主设置，必须遵守）：\n1. 不要发收到'), disp = p.indexOf('派活给你的 agent：@supervisor');
    assert.ok(trust >= 0 && rules > trust && disp > rules, tier);
    assert.ok(p.indexOf('\n', trust) + 1 === rules, `${tier}: rules right after trust rules`);
    assert.match(p, /回复 agent 的消息不会叫醒它/);
  }
  const old = buildPrompt({ nonce: 'abcd', group: { ...group, rules: undefined, commanders: undefined }, msgs, trigger, tier: 'restricted', cwd: '/tmp', myNick: 'me', me: 'a_me' });
  assert.doesNotMatch(old, /本群协作规则|派活给你的/);
});

test('buildPrompt: an agent trigger is not auto-notified except in a DM', () => {
  const members = [{ id: 'a_me', nick: 'me', kind: 'agent' }, { id: 'a_sup', nick: 'supervisor', kind: 'agent' }, { id: 'u_o', nick: 'julio', kind: 'user' }];
  const mk = (author, kind) => {
    const trigger = { seq: 5, author };
    const msgs = [{ seq: 5, kind: 'text', text: '@me 做一下', author, createdAt: Date.now(), new: true }];
    return buildPrompt({ nonce: 'abcd', group: { id: 'g_1', name: 'x', kind, members }, msgs, trigger, tier: 'restricted', cwd: '/tmp', myNick: 'me', me: 'a_me' });
  };
  assert.doesNotMatch(mk({ id: 'a_sup', name: 'supervisor', kind: 'agent' }, 'group'), /自动通知/);
  assert.match(mk({ id: 'a_sup', name: 'supervisor', kind: 'agent' }, 'dm'), /自动通知 supervisor/);
  assert.match(mk({ id: 'u_o', name: 'julio', kind: 'user' }, 'group'), /自动通知 julio/);
});

test('failReason names model gateway failures', async () => {
  const { failReason } = await import('./pichat-listen.mjs');
  const r = (e) => failReason(e);
  assert.match(r('ERROR: unexpected status 503 Service Unavailable: Service temporarily unavailable, url: https://tokenhubapi.com.cn/v1/responses'), /模型服务暂时不可用/);
  assert.match(r('Error running remote compact task: unexpected status 502 Bad Gateway'), /模型服务暂时不可用/);
  assert.match(r('API Error: 429 rate_limit_error'), /额度不足或被限流/);
  assert.match(r('401 invalid_api_key'), /鉴权失败/);
  assert.match(r('ERROR: Reconnecting... 5/5 stream disconnected'), /连不上模型服务/);
  assert.match(r('超过 60 分钟'), /运行超时/);
  assert.equal(r('退出码 1 something else'), '运行出错');
});

test('a person @ing the agent must get a reply, an agent may get [SILENT]', () => {
  const group = { id: 'g1', kind: 'dm', name: 'julio', members: [{ id: 'u1', nick: 'julio', kind: 'human' }, { id: 'a1', nick: 'cx', kind: 'agent' }] };
  const base = { nonce: 'n1', group, msgs: [], skipped: 0, tier: 'full', cwd: '/x', bindDir: '', myNick: 'cx', ownerId: 'u1', me: 'a1', tools: false, agent: 'codex' };
  const human = buildPrompt({ ...base, trigger: { seq: 8, kind: 'text', author: { id: 'u1', kind: 'human', name: 'julio' } } });
  assert.match(human, /一定要回复/);
  assert.doesNotMatch(human, /只输出 \[SILENT\]/);
  const agent = buildPrompt({ ...base, trigger: { seq: 9, kind: 'text', author: { id: 'a2', kind: 'agent', name: 'cc' } } });
  assert.match(agent, /只输出 \[SILENT\]/);
});

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const source = fs.readFileSync(path.join(__dirname, '../src/api.ts'), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
function client(status, body) {
  const calls = []; const dispatched = []; const exports = {};
  vm.runInNewContext(compiled, {
    exports, Event, Date, TextEncoder, Intl,
    window: { dispatchEvent: event => dispatched.push(event.type) },
    fetch: async (...args) => { calls.push(args); return { ok: status >= 200 && status < 300, status, json: async () => body }; },
  });
  return { ...exports, calls, dispatched };
}
const helpers = {};
vm.runInNewContext(compiled, {
  exports: helpers, Event, Date, TextEncoder, Intl,
  window: { dispatchEvent() {} },
  fetch: async () => ({ ok: true, status: 200, json: async () => ({}) }),
});
const { formatRetentionCell, deleteConfirmationMatches, validatePathPrefixes } = helpers;

test('new v0.2.0 error codes map to the dedicated Chinese copy without expiring the session', async () => {
  const paths = client(400, { error: { code: 'invalid_path_rules', message: 'server detail' } });
  await assert.rejects(paths.api('/apps/a1', { method: 'PATCH', body: '{}' }), e => e.code === 'invalid_path_rules' && e.message === '路径规则无效');
  assert.deepEqual(paths.dispatched, []);
  const weeks = client(400, { error: { code: 'invalid_weeks', message: 'server detail' } });
  await assert.rejects(weeks.api('/apps/a1/retention?weeks=99'), e => e.code === 'invalid_weeks' && e.message === '周数须为 1–12');
  assert.deepEqual(weeks.dispatched, []);
});

test('formatRetentionCell renders count with rounded percent and an em dash for empty data', () => {
  assert.equal(formatRetentionCell(5, 10), '5 (50%)');
  assert.equal(formatRetentionCell(1, 3), '1 (33%)');
  assert.equal(formatRetentionCell(2, 3), '2 (67%)');
  assert.equal(formatRetentionCell(0, 10), '0 (0%)');
  assert.equal(formatRetentionCell(3, 0), '—');
  assert.equal(formatRetentionCell(null, 10), '—');
  assert.equal(formatRetentionCell(undefined, 0), '—');
});

test('delete confirmation only accepts the exact non-empty application name', () => {
  assert.equal(deleteConfirmationMatches('Sakura 官网', 'Sakura 官网'), true);
  assert.equal(deleteConfirmationMatches('Sakura 官网', 'sakura 官网'), false);
  assert.equal(deleteConfirmationMatches('Sakura 官网', ' Sakura 官网'), false);
  assert.equal(deleteConfirmationMatches('Sakura 官网', ''), false);
  assert.equal(deleteConfirmationMatches('', ''), false);
});

test('path prefix validation mirrors the backend rules for leading slash, doubled slashes and cap', () => {
  assert.equal(validatePathPrefixes([]), '');
  assert.equal(validatePathPrefixes(['/api/v1', '/docs/']), '');
  assert.equal(validatePathPrefixes(Array.from({ length: 50 }, (_, i) => `/${i}`)), '');
  assert.match(validatePathPrefixes(['api/v1']), /以 \/ 开头/);
  assert.equal(validatePathPrefixes(['/a//b']), '');
  assert.match(validatePathPrefixes(['//a']), /连续斜杠/);
  assert.match(validatePathPrefixes(['/' + '界'.repeat(86)]), /256 字节/);
  assert.match(validatePathPrefixes(['/tab\tpath']), /控制字符/);
  assert.match(validatePathPrefixes(Array.from({ length: 51 }, (_, i) => `/${i}`)), /最多 50 项/);
});

test('v0.2.0 endpoints call the documented routes with the documented methods and pass bodies through', async () => {
  const removed = client(200, { ok: true });
  assert.deepEqual(await removed.deleteApp('app one'), { ok: true });
  assert.equal(removed.calls[0][0], '/api/apps/app%20one');
  assert.equal(removed.calls[0][1].method, 'DELETE');

  const rebuilt = client(200, { rebuilt_days: 7, events: 1200, visitors: 340 });
  assert.deepEqual(await rebuilt.rebuildApp('a1', '2026-01-01', '2026-01-31'), { rebuilt_days: 7, events: 1200, visitors: 340 });
  assert.equal(rebuilt.calls[0][0], '/api/apps/a1/rebuild?from=2026-01-01&to=2026-01-31');
  assert.equal(rebuilt.calls[0][1].method, 'POST');

  const retention = client(200, { weeks: 8, cohorts: [{ cohort: '2026-01-05', size: 30, retained: [12, 9] }] });
  const report = await retention.getRetention('a1', 8);
  assert.equal(retention.calls[0][0], '/api/apps/a1/retention?weeks=8');
  assert.deepEqual(report.cohorts, [{ cohort: '2026-01-05', size: 30, retained: [12, 9] }]);

  const metrics = client(200, { timezone: 'Asia/Shanghai', from: '2026-10-24', to: '2026-10-30', totals: { requests: 9 }, apps: [], daily: [] });
  const gateway = await metrics.getMetrics('2026-10-24', '2026-10-30');
  assert.equal(metrics.calls[0][0], '/api/metrics?from=2026-10-24&to=2026-10-30');
  assert.equal(gateway.totals.requests, 9);

  const settingsBody = { version: '0.2.0', statistics_timezone: 'Asia/Shanghai', retention_days: 90, rate_limit_per_second: 20, rate_limit_burst: 40 };
  const settings = client(200, settingsBody);
  assert.deepEqual(await settings.getSettings(), settingsBody);
  assert.equal(settings.calls[0][0], '/api/settings');
});

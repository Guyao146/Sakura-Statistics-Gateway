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
    exports, Event, Date,
    window: { dispatchEvent: event => dispatched.push(event.type) },
    fetch: async (...args) => { calls.push(args); return { ok: status >= 200 && status < 300, status, json: async () => body }; },
  });
  return { ...exports, calls, dispatched };
}
test('status preserves real backend 503 status document', async () => {
  const body = { status: 'unavailable', database: 'unavailable', time: '2026-10-30T10:00:00Z' };
  const c = client(503, body);
  assert.deepEqual(await c.getStatus(), body);
  assert.equal(c.calls[0][0], '/api/status');
  assert.equal(c.calls[0][1].credentials, 'same-origin');
});
test('structured backend errors preserve status and code and expire session', async () => {
  const c = client(401, { error: { code: 'unauthorized', message: 'Sign in required' } });
  await assert.rejects(c.api('/apps'), e => e.status === 401 && e.code === 'unauthorized' && e.message.includes('登录'));
  assert.deepEqual(c.dispatched, ['sakura:unauthorized']);
});
test('unknown backend error retains server message; login failure does not dispatch expiry', async () => {
  const c = client(400, { error: { code: 'new_backend_code', message: 'Specific server message' } });
  await assert.rejects(c.api('/apps'), e => e.code === 'new_backend_code' && e.message === 'Specific server message');
  const login = client(401, { error: { code: 'invalid_credentials', message: 'Invalid username or password' } });
  await assert.rejects(login.api('/auth/login'));
  assert.deepEqual(login.dispatched, []);
});
test('nullable last_event_at remains null and empty statistics arrays stay arrays', async () => {
  const body = { app: { id: 'a', name: 'A', type: 'web', enabled: true, allowed_origins: [], created_at: '2026-10-30T00:00:00Z', last_event_at: null } };
  const c = client(200, body); assert.equal((await c.api('/apps/a')).app.last_event_at, null);
  const stats = client(200, { versions: [], pages: [], daily: [], last_event_at: null });
  const result = await stats.api('/apps/a/stats');
  assert.equal(result.last_event_at, null); assert.deepEqual(result.versions, []); assert.deepEqual(result.pages, []);
});

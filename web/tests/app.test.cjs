const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const { JSDOM } = require('jsdom');

const sfc = fs.readFileSync(path.join(__dirname, '../src/App.vue'), 'utf8');
const script = sfc.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1];
const exposed = 'export { navigate, openGuide, load, copyText, copy, view, guideSection, selectedId, selected, stats, status, apps, loading, error, actionError, from, to, title, headingLabel, headingDescription, canCreateApp, newOpen, copied, notice };';
const compiled = ts.transpileModule(`${script}\n${exposed}`, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const sections = { web: 'guide-网页接入', desktop: 'guide-桌面客户端接入' };
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function app(options = {}) {
  const calls = [];
  const exports = {};
  const dom = new JSDOM('<pre><code>  原始代码 &amp; 文字</code></pre>');
  const client = {
    api: route => { calls.push(route); return options.api ? options.api(route) : Promise.resolve({ apps: [] }); },
    getStatus: () => { calls.push('/api/status'); return options.status ? options.status() : Promise.resolve({ status: 'ok' }); },
    ApiError: class extends Error {},
    message: e => e.message || String(e),
    localDate: date => date.toISOString().slice(0, 10),
    rangeDays: (from, to) => (Date.parse(to) - Date.parse(from)) / 86400000 + 1,
  };
  vm.runInNewContext(compiled, {
    exports, Date, URL, TextEncoder, Intl, location: new URL('https://console.example.test/'),
    window: dom.window, document: dom.window.document,
    navigator: { clipboard: { writeText: options.writeText || (() => Promise.resolve()) } },
    setTimeout: () => 1, clearTimeout() {},
    require: name => {
      if (name === 'vue') return { ref: value => ({ value }), computed: read => ({ get value() { return read(); } }), onMounted() {}, onBeforeUnmount() {} };
      if (name === './api') return client;
      if (name === './guide') return { guideSections: sections };
      if (name.endsWith('.vue')) return {};
      throw new Error(`Unexpected import ${name}`);
    },
  });
  return { ...exports, calls, dom };
}

test('guide navigation is request-free, separates section from selectedId and clears loading/errors/actions', async () => {
  const c = app();
  c.selectedId.value = 'previous-app';
  c.selected.value = { id: 'previous-app', name: '旧应用' };
  c.stats.value = { daily: [] };
  c.status.value = { status: 'ok' };
  c.loading.value = true;
  c.error.value = '旧错误';
  c.actionError.value = '旧操作错误';
  c.newOpen.value = true;
  c.from.value = '';
  await c.navigate('guide', 'must-not-be-app-id', sections.web);
  assert.equal(c.calls.length, 0);
  assert.equal(c.view.value, 'guide');
  assert.equal(c.guideSection.value, sections.web);
  assert.equal(c.selectedId.value, '');
  assert.equal(c.selected.value, null);
  assert.equal(c.stats.value, null);
  assert.equal(c.status.value, null);
  assert.equal(c.loading.value, false);
  assert.equal(c.error.value, '');
  assert.equal(c.actionError.value, '');
  assert.equal(c.newOpen.value, false);
  assert.equal(c.title.value, '接入指南');
  assert.equal(c.headingLabel.value, 'INTEGRATION GUIDE');
  assert.ok(c.headingDescription.value.includes('接入'));
  assert.equal(c.canCreateApp.value, false);
  await c.load();
  assert.equal(c.calls.length, 0);
});

test('detail links select web/desktop main sections and returning to apps resumes normal loading', async () => {
  const c = app();
  await c.openGuide('web');
  assert.equal(c.guideSection.value, sections.web);
  await c.openGuide('desktop');
  assert.equal(c.guideSection.value, sections.desktop);
  assert.equal(c.selectedId.value, '');
  assert.equal(c.calls.length, 0);
  await c.navigate('apps');
  assert.deepEqual(c.calls, ['/apps']);
  assert.equal(c.guideSection.value, '');
  assert.equal(c.title.value, '应用管理');
  assert.equal(c.canCreateApp.value, true);
});

for (const scenario of [
  { label: 'overview', view: 'overview', id: '' },
  { label: 'application detail', view: 'overview', id: 'app-one' },
  { label: 'apps', view: 'apps', id: '' },
  { label: 'status', view: 'status', id: '' },
]) {
  test(`entering guide ignores an in-flight ${scenario.label} response`, async () => {
    const pending = deferred();
    const c = app({ api: () => pending.promise, status: () => pending.promise });
    c.view.value = scenario.view;
    c.selectedId.value = scenario.id;
    const loading = c.load();
    assert.equal(c.loading.value, true);
    const previousCalls = c.calls.length;
    c.from.value = '';
    await c.navigate('guide');
    assert.equal(c.calls.length, previousCalls);
    assert.equal(c.loading.value, false);
    pending.resolve({ app: { id: 'app-one', name: '旧应用' }, apps: [{ id: 'old' }], daily: [], status: 'ok' });
    await loading;
    assert.equal(c.view.value, 'guide');
    assert.equal(c.selected.value, null);
    assert.equal(c.stats.value, null);
    assert.equal(c.status.value, null);
    assert.equal(c.apps.value.length, 0);
    assert.equal(c.error.value, '');
    assert.equal(c.loading.value, false);
  });
}

test('a stale rejection cannot replace guide content or overwrite newer loading state', async () => {
  const old = deferred();
  const current = deferred();
  let phase = 'old';
  const c = app({ api: () => phase === 'old' ? old.promise : current.promise });
  const previousLoad = c.load();
  await c.navigate('guide');
  phase = 'current';
  const nextLoad = c.navigate('apps');
  assert.equal(c.loading.value, true);
  old.reject(new Error('旧请求失败'));
  await previousLoad;
  assert.equal(c.error.value, '');
  assert.equal(c.loading.value, true);
  current.resolve({ apps: [{ id: 'fresh' }] });
  await nextLoad;
  assert.equal(c.apps.value[0].id, 'fresh');
  assert.equal(c.loading.value, false);
});

test('copy waits for Clipboard API resolution before reporting success', async () => {
  const clipboard = deferred();
  const writes = [];
  const c = app({ writeText: content => { writes.push(content); return clipboard.promise; } });
  const content = '  <head> & 中文\n';
  const copying = c.copyText(content, c.dom.window.document.querySelector('code'));
  assert.deepEqual(writes, [content]);
  assert.equal(c.notice.value, '');
  clipboard.resolve();
  assert.equal(await copying, true);
  assert.equal(c.notice.value, '代码已复制');
  assert.equal(c.actionError.value, '');
});

test('Clipboard API rejection reports failure and selects code for manual copying', async () => {
  const c = app({ writeText: () => Promise.reject(new Error('剪贴板不可用')) });
  const code = c.dom.window.document.querySelector('code');
  assert.equal(await c.copyText('  原始代码 & 文字', code), false);
  assert.equal(c.notice.value, '');
  assert.ok(c.actionError.value.includes('手动复制'));
  assert.equal(c.dom.window.getSelection().toString(), code.textContent);
});

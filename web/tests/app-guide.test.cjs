const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const vue = require('vue');
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc');
const { JSDOM } = require('jsdom');

const appSource = fs.readFileSync(path.join(__dirname, '../src/App.vue'), 'utf8');
const { descriptor } = parse(appSource);
const script = descriptor.scriptSetup.content;
const stateNames = 'view, guideSection, selectedId, selected, stats, apps, status, settings, collect, loading, error, actionError, title, headingLabel, headingDescription, canCreateApp, newOpen, notice, copied, from, to, rangeError, navigate, openGuide, load, copyText, copyGuideCode, cards, busy, rebuildOpen, rebuildFrom, rebuildTo, rebuildError, openRebuild, confirmRebuild, deleteOpen, deleteName, deleteError, canDelete, openDelete, confirmDelete, retention, retentionToday, retentionCell, format, when';
const compiled = ts.transpileModule(`${script}\nexport { ${stateNames} };`, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
}).outputText;
const bindingMetadata = compileScript(descriptor, { id: 'test-app' }).bindings;
const template = compileTemplate({
  source: descriptor.template.content, filename: 'App.vue', id: 'test-app',
  compilerOptions: { bindingMetadata },
});
assert.deepEqual(template.errors, []);
const renderSource = ts.transpileModule(template.code, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const templateExports = {};
vm.runInNewContext(renderSource, { exports: templateExports, require });

function app(options = {}) {
  const calls = [];
  const dom = new JSDOM('<pre><code>手动复制的代码</code></pre>');
  const lifecycle = [];
  const exports = {};
  const api = async url => {
    calls.push(url);
    if (options.api) return options.api(url);
    throw new Error(`Unexpected request: ${url}`);
  };
  vm.runInNewContext(compiled, {
    exports, URL, Date, TextEncoder,
    location: new URL('https://console.example.test/'),
    navigator: { clipboard: { writeText: options.writeText || (async () => {}) } },
    document: dom.window.document, window: dom.window,
    setTimeout: () => 1, clearTimeout() {},
    require: name => {
      if (name === 'vue') return { ...vue, onMounted: fn => lifecycle.push(fn), onBeforeUnmount() {} };
      if (name === './guide') return { guideSections: { web: 'guide-网页接入', desktop: 'guide-桌面客户端接入' } };
      if (name === './api') return {
        api, getStatus: () => api('/status'), ApiError: class ApiError extends Error {},
        getSettings: options.getSettings || (() => api('/settings')),
        getMetrics: options.getMetrics || (() => api('/metrics')),
        getRetention: async () => ({ weeks: 4, cohorts: [] }),
        rebuildApp: options.rebuildApp || (() => { throw new Error('Unexpected rebuild'); }),
        deleteApp: options.deleteApp || (() => { throw new Error('Unexpected deletion'); }),
        deleteConfirmationMatches: (name, input) => name !== '' && name === input,
        dateInTimezone: date => date.toISOString().slice(0, 10),
        retentionWeekState: (cohort, index, today) => {
          const start = Date.parse(cohort) + index * 7 * 86400000;
          const day = Date.parse(today);
          return start > day ? 'future' : day < start + 7 * 86400000 ? 'current' : 'complete';
        },
        formatRetentionCell: (count, size) => !size || count == null ? '—' : `${count} (${Math.round(count * 100 / size)}%)`,
        message: e => e.message, localDate: date => date.toISOString().slice(0, 10),
        rangeDays: (from, to) => (Date.parse(to) - Date.parse(from)) / 86400000 + 1,
      };
      if (name.endsWith('.vue')) return {};
      throw new Error(`Unexpected import: ${name}`);
    },
  });
  return { ...exports, calls, dom, lifecycle };
}

function pending() {
  let resolve; let reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function rendered(state, additions = {}) {
  const setup = vue.proxyRefs({
    ...state, booting: false, user: 'admin', busy: false,
    IntegrationGuide: 'integration-guide', TrendChart: 'trend-chart', ...additions,
  });
  return templateExports.render({}, [], {}, setup, {}, {});
}

function find(node, predicate) {
  if (!node || typeof node !== 'object') return [];
  const nodes = predicate(node) ? [node] : [];
  if (Array.isArray(node.children)) for (const child of node.children) nodes.push(...find(child, predicate));
  return nodes;
}

function text(node) {
  if (typeof node === 'string') return node;
  if (!node || typeof node !== 'object') return '';
  return typeof node.children === 'string' ? node.children : Array.isArray(node.children) ? node.children.map(text).join('') : '';
}

test('guide load clears state before date validation and never requests API even with a leftover app ID', async () => {
  const state = app();
  state.view.value = 'guide'; state.selectedId.value = 'leftover';
  state.loading.value = true; state.error.value = '旧错误'; state.actionError.value = '旧操作错误';
  state.from.value = ''; state.to.value = '';
  await state.load();
  assert.equal(state.calls.length, 0);
  assert.equal(state.loading.value, false);
  assert.equal(state.error.value, ''); assert.equal(state.actionError.value, '');
});

test('web and desktop guide navigation use independent section state and return to apps normally', async () => {
  const state = app({ api: async () => ({ apps: [] }) });
  state.selectedId.value = 'web-app'; state.selected.value = { id: 'web-app', name: '网站', type: 'web' };
  state.newOpen.value = true;
  await state.openGuide('web');
  assert.equal(state.view.value, 'guide'); assert.equal(state.guideSection.value, 'guide-网页接入');
  assert.equal(state.selectedId.value, ''); assert.equal(state.selected.value, null);
  assert.equal(state.title.value, '接入指南'); assert.equal(state.headingLabel.value, 'INTEGRATION GUIDE');
  assert.equal(state.canCreateApp.value, false); assert.equal(state.newOpen.value, false);
  await state.openGuide('desktop');
  assert.equal(state.guideSection.value, 'guide-桌面客户端接入');
  assert.equal(state.calls.length, 0);
  await state.navigate('apps');
  assert.equal(state.guideSection.value, ''); assert.equal(state.canCreateApp.value, true);
  assert.deepEqual(state.calls, ['/apps']);
});

test('late detail responses cannot restore statistics or selected app after switching to guide', async () => {
  const details = pending(); const stats = pending();
  const state = app({ api: url => url.includes('/stats?') ? stats.promise : details.promise });
  state.selectedId.value = 'app-one';
  const loading = state.load();
  assert.equal(state.calls.length, 2); assert.equal(state.loading.value, true);
  await state.navigate('guide');
  details.resolve({ app: { id: 'app-one', name: '旧应用', type: 'web' } });
  stats.resolve({ totals: { active: 1 }, daily: [] });
  await loading;
  assert.equal(state.selected.value, null); assert.equal(state.stats.value, null);
  assert.equal(state.loading.value, false); assert.equal(state.error.value, '');
  assert.equal(state.calls.length, 2);
});

test('late overview errors cannot replace the guide with an error panel', async () => {
  const response = pending();
  const state = app({ api: () => response.promise });
  const loading = state.load();
  await state.navigate('guide');
  response.reject(new Error('旧请求失败'));
  await loading;
  assert.equal(state.view.value, 'guide'); assert.equal(state.error.value, '');
  assert.equal(state.loading.value, false); assert.equal(state.apps.value.length, 0);
});

test('compiled guide UI has a named mobile nav button, guide body, no statistical filters or new-app actions', async () => {
  const state = app();
  await state.navigate('guide');
  const tree = rendered(state);
  const buttons = find(tree, node => node.type === 'button');
  const guideButton = buttons.find(button => button.props?.['aria-label'] === '接入指南');
  assert.ok(guideButton); assert.equal(guideButton.props.title, '接入指南');
  assert.equal(find(tree, node => node.type === 'integration-guide').length, 1);
  assert.equal(find(tree, node => node.props?.class === 'filter-bar').length, 0);
  assert.equal(buttons.filter(button => /新建应用|刷新状态/.test(text(button))).length, 0);
  assert.equal(find(tree, node => node.type === 'h1').map(text).join(''), '接入指南');
});

test('copy reports success only after the Clipboard promise resolves and passes the original text', async () => {
  const clipboard = pending(); const calls = [];
  const state = app({ writeText: content => { calls.push(content); return clipboard.promise; } });
  const content = '  const text = "<head> & 中文";\n';
  const copying = state.copyGuideCode(content, state.dom.window.document.querySelector('code'));
  assert.deepEqual(calls, [content]); assert.equal(state.notice.value, '');
  clipboard.resolve(); await copying;
  assert.equal(state.notice.value, '代码已复制'); assert.equal(state.actionError.value, '');
});

test('failed Clipboard promises do not report success and select code for manual copying', async () => {
  const state = app({ writeText: async () => { throw new Error('Clipboard unavailable'); } });
  const code = state.dom.window.document.querySelector('code');
  const result = await state.copyText('手动复制的代码', code);
  assert.equal(result, false); assert.equal(state.notice.value, '');
  assert.match(state.actionError.value, /手动复制/);
  assert.equal(state.dom.window.getSelection().toString(), '手动复制的代码');
});

test('rebuild confirmation captures dates and waits for the response', async () => {
  const response = pending(); const calls = [];
  const state = app({
    api: async route => route.includes('/stats?') ? { daily: [], totals: {} } : { app: { id: 'app-one', name: '示例网站', type: 'web', allowed_path_prefixes: [] } },
    rebuildApp: (...args) => { calls.push(args); return response.promise; },
  });
  state.selectedId.value = 'app-one'; state.selected.value = { id: 'app-one', name: '示例网站' };
  state.from.value = '2026-11-01'; state.to.value = '2026-11-02';
  state.openRebuild();
  assert.equal(state.rebuildOpen.value, true);
  const operation = state.confirmRebuild();
  assert.deepEqual(calls, [['app-one', '2026-11-01', '2026-11-02']]);
  assert.equal(state.busy.value, true); assert.equal(state.notice.value, '');
  response.resolve({ rebuilt_days: 2, events: 4, visitors: 2 });
  await operation;
  assert.equal(state.rebuildOpen.value, false); assert.equal(state.busy.value, false);
  assert.match(state.notice.value, /已重建 2 天统计/);
});

test('a protected rebuild retains the dialog without a success notice', async () => {
  const state = app({ rebuildApp: async () => { throw new Error('日期超出原始事件保留范围，无法安全重建'); } });
  state.selected.value = { id: 'app-one', name: '示例网站' };
  state.openRebuild();
  await state.confirmRebuild();
  assert.equal(state.rebuildOpen.value, true); assert.equal(state.busy.value, false);
  assert.match(state.rebuildError.value, /无法安全重建/);
  assert.equal(state.notice.value, '');
});

test('delete requires the exact name before sending a request', async () => {
  const calls = [];
  const state = app({ api: async () => ({ apps: [] }), deleteApp: async id => { calls.push(id); return { ok: true }; } });
  state.selected.value = { id: 'app-one', name: '示例网站' };
  state.openDelete(); state.deleteName.value = '错误名称';
  await state.confirmDelete();
  assert.deepEqual(calls, []); assert.equal(state.deleteOpen.value, true);
  state.deleteName.value = '示例网站';
  await state.confirmDelete();
  assert.deepEqual(calls, ['app-one']); assert.equal(state.deleteOpen.value, false);
  assert.equal(state.view.value, 'apps'); assert.equal(state.notice.value, '应用已删除');
});

test('six overview metrics include custom events in the compiled UI', () => {
  const state = app();
  state.stats.value = { totals: { active: 1, starts: 1, sessions: 1, page_views: 1, new_visitors: 1, custom_events: 7 }, daily: [], apps: [] };
  const articles = find(rendered(state, { cards: state.cards }), node => node.type === 'article');
  assert.equal(articles.length, 6);
  assert.match(articles.map(text).join(' '), /自定义事件/);
  assert.match(text(articles[5]), /7/);
});

test('retention uses server dates and distinguishes ongoing and future weeks', () => {
  const state = app();
  state.retention.value = { weeks: 4, timezone: 'Asia/Shanghai', today: '2026-11-02', cohorts: [] };
  const cohort = { cohort: '2026-10-26', size: 10, retained: [10, 3, 0, 0] };
  assert.equal(state.retentionCell(cohort, 1), '10 (100%)');
  assert.equal(state.retentionCell(cohort, 2), '3 (30%) · 进行中');
  assert.equal(state.retentionCell(cohort, 3), '—');
});

test('stale status responses do not start more requests after entering guide', async () => {
  const response = pending(); let extra = 0;
  const state = app({ api: () => response.promise, getSettings: async () => { extra++; return {}; }, getMetrics: async () => { extra++; return {}; } });
  state.view.value = 'status';
  const operation = state.load();
  await state.navigate('guide');
  response.resolve({ status: 'ok', database: 'ok' });
  await operation;
  assert.equal(extra, 0); assert.equal(state.view.value, 'guide');
});

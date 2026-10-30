const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const { randomUUID, webcrypto } = require('node:crypto');
const source = fs.readFileSync(require('node:path').join(__dirname, '../public/sdk.js'), 'utf8');
function storage() {
  const data = new Map();
  return { get length() { return data.size; }, key: i => [...data.keys()][i], getItem: k => data.get(k) ?? null, setItem: (k, v) => data.set(k, String(v)), removeItem: k => data.delete(k), data };
}
function browser(options = {}) {
  const store = options.store || storage();
  const calls = []; const docEvents = {}; const winEvents = {}; const timers = [];
  let now = Date.parse(options.now || '2026-10-30T10:00:00Z');
  const location = new URL('https://site.example/home?private=secret');
  const attrs = { 'data-app-id': 'app-1', 'data-endpoint': 'https://stats.example/api/collect', ...options.attrs };
  const document = { visibilityState: 'visible', currentScript: { src: 'https://stats.example/sdk.js', getAttribute: k => attrs[k] || null }, addEventListener: (n, f) => (docEvents[n] ||= []).push(f) };
  const window = {
    location, crypto: { randomUUID, getRandomValues: webcrypto.getRandomValues.bind(webcrypto) },
    fetch: async (url, init) => { calls.push({ url, init, body: JSON.parse(init.body) }); if (options.fetch) return options.fetch(url, init); return { ok: true, status: 200, json: async () => ({ accepted: 0, duplicates: 1, rejected: 1 }) }; },
    addEventListener: (n, f) => (winEvents[n] ||= []).push(f),
    setInterval: f => { timers.push(f); return timers.length; }, setTimeout: () => 1, clearTimeout() {},
    history: { pushState(_s, _t, url) { if (url) location.href = new URL(url, location.href).href; return 'original'; }, replaceState(_s, _t, url) { if (url) location.href = new URL(url, location.href).href; } },
    navigator: { sendBeacon: (...args) => { window.beacons.push(args); return true; } }, beacons: [],
  };
  Object.defineProperty(window, 'localStorage', { get() { if (options.storageThrows) throw new Error('Storage disabled'); return store; } });
  class Clock extends Date { constructor(...args) { super(...(args.length ? args : [now])); } static now() { return now; } }
  const context = vm.createContext({ window, document, URL, Date: Clock, Uint8Array, Map, Math, Promise, Blob, console });
  vm.runInContext(source, context);
  return { window, document, store, calls, sdk: window.SakuraStats, advance: ms => { now += ms; }, tick: async () => { timers.forEach(f => f()); await new Promise(resolve => setImmediate(resolve)); }, doc: (n, e = {}) => (docEvents[n] || []).forEach(f => f(e)), event: (n, e = {}) => (winEvents[n] || []).forEach(f => f(e)), reload: () => vm.runInContext(source, context) };
}
const events = b => b.calls.flatMap(c => c.body.events);
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

test('works without browser storage; UUID protocol and pathname privacy', async () => {
  const b = browser({ storageThrows: true }); await b.sdk.flush();
  assert.equal(events(b).length, 2);
  for (const e of events(b)) { assert.match(e.anonymous_id, uuid); assert.match(e.session_id, uuid); assert.match(e.event_id, uuid); assert.equal(e.sdk_version, '0.1.0'); assert.equal(e.schema_version, 1); }
  assert.equal(events(b)[0].path, '/home'); assert.equal(b.calls[0].init.credentials, 'omit');
  b.reload(); b.sdk.trackPage('/next?token=hidden'); await b.sdk.flush();
  assert.equal(events(b).filter(e => e.path === '/next').length, 1);
});
test('successful duplicate/rejected acknowledgement drops entire batch', async () => {
  const b = browser(); await b.sdk.flush(); await b.sdk.flush();
  assert.equal(b.calls.length, 1);
  assert.equal([...b.store.data.keys()].filter(k => k.includes(':event:')).length, 0);
});
test('network failures retry the same event IDs after backoff', async () => {
  let attempt = 0;
  const b = browser({ fetch: async () => { if (++attempt === 1) throw new Error('offline'); return { ok: true, status: 200 }; } });
  await b.sdk.flush(); await b.sdk.flush(); assert.equal(b.calls.length, 1);
  b.advance(3000); await b.sdk.flush();
  assert.deepEqual(b.calls[0].body, b.calls[1].body);
  await b.sdk.flush(); assert.equal(b.calls.length, 2);
});
test('permanent HTTP rejection terminates; server failure and rate limiting retain IDs', async () => {
  for (const status of [400, 403, 422, 429, 503]) {
    const b = browser({ fetch: async () => ({ ok: false, status }) });
    await b.sdk.flush(); b.advance(65000); await b.sdk.flush();
    assert.equal(b.calls.length, [429, 503].includes(status) ? 2 : 1);
    if (b.calls.length === 2) assert.deepEqual(b.calls[0].body, b.calls[1].body);
  }
});
test('shared tabs reuse session and merge active events within five minutes', async () => {
  const store = storage(); const a = browser({ store }); const b = browser({ store });
  await b.sdk.flush();
  const all = events(b); assert.equal(all.filter(e => e.event_type === 'active').length, 1);
  assert.equal(new Set(all.map(e => e.session_id)).size, 1); assert.equal(new Set(all.map(e => e.anonymous_id)).size, 1);
  a.advance(31 * 60000); a.doc('pointerdown', { isTrusted: true }); await a.sdk.flush();
  assert.notEqual(events(a).at(-1).session_id, all[0].session_id);
});
test('active requires visibility and recent trusted interaction; coalesces at five minutes', async () => {
  const b = browser(); await b.sdk.flush();
  b.advance(6 * 60000); await b.tick(); assert.equal(events(b).filter(e => e.event_type === 'active').length, 1);
  b.document.visibilityState = 'hidden'; b.doc('pointerdown', { isTrusted: true }); await b.tick(); assert.equal(b.calls.length, 1);
  b.document.visibilityState = 'visible'; b.doc('pointerdown', { isTrusted: false }); await b.tick(); assert.equal(b.calls.length, 1);
  b.doc('pointerdown', { isTrusted: true }); await b.sdk.flush(); assert.equal(events(b).filter(e => e.event_type === 'active').length, 2);
  b.doc('scroll', { isTrusted: true }); await b.sdk.flush(); assert.equal(b.calls.length, 2);
});
test('trusted interaction crosses Shanghai midnight within five minutes and keeps session', async () => {
  const b = browser({ now: '2026-10-30T15:59:30Z' }); await b.sdk.flush();
  const first = events(b).find(e => e.event_type === 'active');
  b.advance(60000); b.doc('pointerdown', { isTrusted: true }); await b.sdk.flush();
  const active = events(b).filter(e => e.event_type === 'active');
  assert.equal(active.length, 2); assert.equal(active[1].session_id, first.session_id);
  b.doc('pointerdown', { isTrusted: true }); await b.sdk.flush(); assert.equal(b.calls.length, 2);
});
test('configured timezone controls midnight instead of UTC or Shanghai default', async () => {
  const b = browser({ now: '2026-10-30T03:59:30Z', attrs: { 'data-timezone': 'America/New_York' } });
  await b.sdk.flush(); b.advance(60000); b.doc('keydown', { isTrusted: true }); await b.sdk.flush();
  assert.equal(events(b).filter(e => e.event_type === 'active').length, 2);
});
test('invalid timezone falls back to Shanghai and disable rotates anonymous identity', async () => {
  const b = browser({ now: '2026-10-30T15:59:30Z', attrs: { 'data-timezone': 'Invalid/Zone' } });
  await b.sdk.flush(); const oldId = events(b)[0].anonymous_id;
  b.advance(60000); b.doc('keydown', { isTrusted: true }); await b.sdk.flush();
  assert.equal(events(b).filter(e => e.event_type === 'active').length, 2);
  b.sdk.disable(); b.sdk.enable(); await b.sdk.flush(); assert.notEqual(events(b).at(-1).anonymous_id, oldId);
});
test('automatic route updates do not count as user interaction for active', async () => {
  const b = browser(); await b.sdk.flush(); b.advance(6 * 60000);
  b.window.history.pushState(null, '', '/background-update'); await b.sdk.flush();
  assert.equal(events(b).filter(e => e.event_type === 'page_view').length, 2);
  assert.equal(events(b).filter(e => e.event_type === 'active').length, 1);
});
test('history deduplicates query and anchor changes; opt-in hash router strips query', async () => {
  const b = browser(); assert.equal(b.window.history.pushState(null, '', '/next?q=1'), 'original');
  b.window.history.replaceState(null, '', '/next?q=2#anchor'); b.event('popstate'); await b.sdk.flush();
  assert.deepEqual(events(b).filter(e => e.event_type === 'page_view').map(e => e.path), ['/home', '/next']);
  const h = browser({ attrs: { 'data-hash-mode': 'route' } }); h.window.location.hash = '#/settings?private=1'; h.event('hashchange'); await h.sdk.flush();
  assert.equal(events(h).at(-1).path, '/home/~hash/settings');
  h.window.location.hash = '#/profile?private=2'; h.event('hashchange'); await h.sdk.flush();
  const paths = events(h).filter(e => e.event_type === 'page_view').map(e => new URL(e.path, 'https://server.example').pathname);
  assert.deepEqual(paths, ['/home', '/home/~hash/settings', '/home/~hash/profile']);
  h.window.location.hash = '#anchor'; h.event('hashchange'); await h.sdk.flush();
  assert.equal(events(h).at(-1).path, '/home');
});
test('opt out clears persisted queue and identifiers and survives reload; enable resumes', async () => {
  const b = browser(); b.sdk.disable(); b.sdk.trackPage('/secret'); await b.sdk.flush(); b.event('pagehide');
  assert.equal(b.calls.length, 0); assert.equal(b.window.beacons.length, 0);
  assert.deepEqual([...b.store.data.keys()], ['sakura:app-1:disabled']);
  const c = browser({ store: b.store }); await c.sdk.flush(); assert.equal(c.calls.length, 0);
  c.sdk.enable(); await c.sdk.flush(); assert.equal(events(c).length, 2);
});
test('queue is bounded and survives reload; pagehide beacon retains IDs until acknowledgement', async () => {
  const b = browser(); for (let i = 0; i < 250; i++) b.sdk.trackPage('/p/' + i);
  assert.equal([...b.store.data.keys()].filter(k => k.includes(':event:')).length, 200);
  b.event('pagehide'); assert.equal(b.window.beacons.length, 1);
  assert.equal([...b.store.data.keys()].filter(k => k.includes(':event:')).length, 200);
  const c = browser({ store: b.store }); await c.sdk.flush();
  assert.equal(events(c).length, 30); assert.equal(events(c)[0].path, '/p/51');
});
test('malformed storage and write failures do not interrupt collection', async () => {
  const store = storage(); store.setItem('sakura:app-1:session', '{'); store.setItem('sakura:app-1:event:bad', '{}');
  store.setItem = () => { throw new Error('Quota exceeded'); };
  const b = browser({ store }); await b.sdk.flush(); assert.equal(events(b).length, 2);
});
test('disable cancels a flush scheduled in the same turn', async () => {
  const b = browser(); const pending = b.sdk.flush(); b.sdk.disable(); await pending;
  assert.equal(b.calls.length, 0);
});
test('concurrent flush calls share one request and preserve newly queued events', async () => {
  let release;
  const b = browser({ fetch: () => new Promise(resolve => { release = resolve; }) });
  const first = b.sdk.flush(); const second = b.sdk.flush(); assert.equal(first, second);
  await Promise.resolve(); b.sdk.trackPage('/during-request');
  release({ ok: true, status: 200 }); await first;
  const pending = b.sdk.flush(); await Promise.resolve(); release({ ok: true, status: 200 }); await pending;
  assert.equal(b.calls.length, 2); assert.equal(b.calls[1].body.events[0].path, '/during-request');
});
test('cross-tab acknowledged deletion does not resurrect already sent events', async () => {
  const store = storage(); const a = browser({ store }); const b = browser({ store });
  const ids = [...store.data.keys()].filter(k => k.includes(':event:'));
  await b.sdk.flush(); ids.forEach(key => a.event('storage', { key, newValue: null }));
  await a.sdk.flush(); assert.equal(a.calls.length, 0);
});

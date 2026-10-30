/* Sakura Statistics SDK 0.1.0 — public application IDs are not security credentials. */
(function (window, document) {
  'use strict';
  if (window.SakuraStats) return;
  var script = document.currentScript;
  if (!script) return;
  var appId = script.getAttribute('data-app-id');
  if (!appId) return;
  var endpoint;
  try {
    endpoint = new URL(script.getAttribute('data-endpoint') || '/api/collect', script.src || window.location.href);
    if (!/^https?:$/.test(endpoint.protocol)) return;
  } catch (_) { return; }
  var timezone = script.getAttribute('data-timezone') || 'Asia/Shanghai';
  var dayFormatter;
  try { dayFormatter = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit' }); }
  catch (_) { dayFormatter = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }); }
  function statisticsDay(now) { return dayFormatter.format(new Date(now)); }
  var SESSION_TTL = 30 * 60 * 1000;
  var ACTIVE_INTERVAL = 5 * 60 * 1000;
  var INTERACTION_WINDOW = 60 * 1000;
  var MAX_QUEUE = 200;
  var BATCH_SIZE = 30;
  var MAX_AGE = 7 * 86400000;
  var prefix = 'sakura:' + encodeURIComponent(appId) + ':';
  var outbox = prefix + 'event:';
  var memory = new Map();
  var storage;
  try { storage = window.localStorage; } catch (_) { storage = null; }
  function get(key) { try { return storage ? storage.getItem(key) : null; } catch (_) { return null; } }
  function put(key, value) { try { if (storage) storage.setItem(key, value); } catch (_) {} }
  function remove(key) { try { if (storage) storage.removeItem(key); } catch (_) {} }
  function keys() {
    var result = [];
    try { if (storage) for (var i = 0; i < storage.length; i++) { var key = storage.key(i); if (key && key.indexOf(outbox) === 0) result.push(key); } } catch (_) {}
    return result;
  }
  function uuid() {
    try { if (window.crypto.randomUUID) return window.crypto.randomUUID(); } catch (_) {}
    var bytes = new Uint8Array(16);
    try { window.crypto.getRandomValues(bytes); } catch (_) { for (var i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256); }
    bytes[6] = (bytes[6] & 15) | 64; bytes[8] = (bytes[8] & 63) | 128;
    var hex = Array.from(bytes, function (b) { return b.toString(16).padStart(2, '0'); }).join('');
    return hex.slice(0, 8) + '-' + hex.slice(8, 12) + '-' + hex.slice(12, 16) + '-' + hex.slice(16, 20) + '-' + hex.slice(20);
  }
  var uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
  var anonymousId = get(prefix + 'anonymous');
  if (!uuidPattern.test(anonymousId || '')) anonymousId = uuid();
  var session = null;
  var disabled = get(prefix + 'disabled') === '1';
  var inFlight = null;
  var epoch = 0;
  var failures = 0;
  var nextAttempt = 0;
  var lastInteraction = Date.now();
  var lastPath = '';
  var hashRoutes = script.getAttribute('data-hash-mode') === 'route';
  function isDisabled() {
    if (get(prefix + 'disabled') === '1') disabled = true;
    return disabled;
  }
  function visible() { return document.visibilityState === 'visible'; }
  function identify(now) {
    put(prefix + 'anonymous', anonymousId);
    try {
      var shared = JSON.parse(get(prefix + 'session') || 'null');
      if (shared && uuidPattern.test(shared.id) && Number.isFinite(shared.touched) && shared.touched <= now && now - shared.touched < SESSION_TTL) session = shared;
    } catch (_) {}
    if (!session || now < session.touched || now - session.touched >= SESSION_TTL) session = { id: uuid(), touched: now, active: 0 };
    session.touched = now;
    put(prefix + 'session', JSON.stringify(session));
    return session;
  }
  function entries() {
    keys().forEach(function (key) {
      try {
        var item = JSON.parse(get(key));
        if (item && item.app_id === appId && item.event && item.event.event_id === key.slice(outbox.length) && Number.isFinite(Date.parse(item.event.occurred_at))) memory.set(item.event.event_id, item.event);
        else remove(key);
      } catch (_) { remove(key); }
    });
    var now = Date.now();
    var all = Array.from(memory.values()).sort(function (a, b) { return Date.parse(a.occurred_at) - Date.parse(b.occurred_at); });
    all.forEach(function (event) { if (now - Date.parse(event.occurred_at) > MAX_AGE || Date.parse(event.occurred_at) > now + 60000) drop(event.event_id); });
    all = all.filter(function (event) { return memory.has(event.event_id); });
    while (all.length > MAX_QUEUE) drop(all.shift().event_id);
    return all;
  }
  function drop(id) { memory.delete(id); remove(outbox + id); }
  function queue(type, path, now) {
    if (isDisabled()) return;
    var current = identify(now);
    var event = { schema_version: 1, event_id: uuid(), event_type: type, occurred_at: new Date(now).toISOString(), anonymous_id: anonymousId, session_id: current.id, platform: 'web', sdk_version: '0.1.0' };
    if (path !== undefined) event.path = path;
    memory.set(event.event_id, event);
    put(outbox + event.event_id, JSON.stringify({ app_id: appId, event: event }));
    entries();
  }
  function active(now) {
    if (isDisabled() || !visible() || now - lastInteraction > INTERACTION_WINDOW) return;
    var current = identify(now);
    var day = statisticsDay(now);
    if (current.active && current.active_day === day && now - current.active < ACTIVE_INTERVAL) return;
    current.active = now;
    current.active_day = day;
    put(prefix + 'session', JSON.stringify(current));
    queue('active', undefined, now);
  }
  function pathFor(input) {
    try {
      var url = new URL(typeof input === 'string' ? input : window.location.href, window.location.href);
      var hash = hashRoutes && /^#\//.test(url.hash) ? '/~hash' + url.hash.slice(1).split(/[?#]/)[0] : '';
      var pathname = hash ? url.pathname.replace(/\/$/, '') : url.pathname;
      return (pathname + hash).slice(0, 2048);
    } catch (_) { return window.location.pathname; }
  }
  function trackPage(input, automatic) {
    if (isDisabled() || !visible()) return;
    var path = pathFor(input);
    if (automatic && path === lastPath) return;
    lastPath = path;
    var now = Date.now();
    queue('page_view', path, now); active(now);
  }
  function flush() {
    if (isDisabled()) return Promise.resolve();
    if (inFlight) return inFlight;
    if (Date.now() < nextAttempt) return Promise.resolve();
    var batch = entries().slice(0, BATCH_SIZE);
    if (!batch.length || typeof window.fetch !== 'function') return Promise.resolve();
    var currentEpoch = epoch;
    var controller = typeof window.AbortController === 'function' ? new window.AbortController() : null;
    var timeout = controller ? window.setTimeout(function () { controller.abort(); }, 15000) : null;
    inFlight = Promise.resolve().then(function () {
      if (currentEpoch !== epoch || isDisabled()) return null;
      return window.fetch(endpoint.href, { method: 'POST', credentials: 'omit', mode: 'cors', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ app_id: appId, events: batch }), signal: controller ? controller.signal : undefined, keepalive: true });
    }).then(function (response) {
      if (currentEpoch !== epoch || !response) return;
      // A successful response acknowledges the entire batch, including duplicates/rejections.
      // Permanent client errors also terminate this batch; 429 and server/network failures retry.
      if (response.ok || (response.status >= 400 && response.status < 500 && response.status !== 429)) {
        batch.forEach(function (event) { drop(event.event_id); }); failures = 0; nextAttempt = 0;
      } else { throw new Error('Temporary collection failure'); }
    }).catch(function () {
      if (currentEpoch !== epoch) return;
      failures++; nextAttempt = Date.now() + Math.min(60000, 1000 * Math.pow(2, Math.min(failures, 6)));
    }).finally(function () { if (timeout !== null) window.clearTimeout(timeout); inFlight = null; });
    return inFlight;
  }
  function clearQueue() { memory.clear(); keys().forEach(remove); }
  function disable() {
    disabled = true; epoch++; put(prefix + 'disabled', '1'); clearQueue();
    remove(prefix + 'anonymous'); remove(prefix + 'session'); session = null; lastPath = '';
  }
  function enable() {
    if (!isDisabled()) return;
    disabled = false; remove(prefix + 'disabled'); anonymousId = uuid(); session = null;
    failures = 0; nextAttempt = 0; lastPath = ''; lastInteraction = Date.now(); trackPage(undefined, true);
  }
  window.SakuraStats = { disable: disable, enable: enable, trackPage: function (path) { trackPage(path, false); }, flush: flush };
  ['pointerdown', 'keydown', 'touchstart', 'scroll'].forEach(function (name) {
    document.addEventListener(name, function (event) {
      if (event.isTrusted === false || !visible() || isDisabled()) return;
      var now = Date.now(); lastInteraction = now; active(now);
    }, { passive: true });
  });
  document.addEventListener('visibilitychange', function () {
    if (visible()) { trackPage(undefined, true); active(Date.now()); }
    else flush();
  });
  // History APIs keep their original return values and receiver semantics.
  ['pushState', 'replaceState'].forEach(function (method) {
    var original = window.history[method];
    if (typeof original !== 'function') return;
    try { window.history[method] = function () { var result = original.apply(this, arguments); trackPage(undefined, true); return result; }; } catch (_) {}
  });
  window.addEventListener('popstate', function () { trackPage(undefined, true); });
  window.addEventListener('hashchange', function () { if (hashRoutes) trackPage(undefined, true); });
  window.addEventListener('storage', function (event) {
    if (event.key === prefix + 'disabled') {
      if (event.newValue === '1') { disabled = true; epoch++; clearQueue(); session = null; lastPath = ''; }
      else { disabled = false; anonymousId = get(prefix + 'anonymous') || uuid(); session = null; lastPath = ''; trackPage(undefined, true); }
    }
    if (event.key && event.key.indexOf(outbox) === 0 && event.newValue === null) memory.delete(event.key.slice(outbox.length));
  });
  window.addEventListener('pagehide', function () {
    if (isDisabled()) return;
    var batch = entries().slice(0, BATCH_SIZE);
    if (!batch.length) return;
    // Beacons have no acknowledgement: retain IDs for safe server deduplication on reload.
    try { if (window.navigator.sendBeacon) window.navigator.sendBeacon(endpoint.href, new Blob([JSON.stringify({ app_id: appId, events: batch })], { type: 'application/json' })); } catch (_) {}
  });
  window.setInterval(function () { active(Date.now()); flush(); }, 10000);
  if (disabled) clearQueue(); else { entries(); trackPage(undefined, true); }
})(window, document);

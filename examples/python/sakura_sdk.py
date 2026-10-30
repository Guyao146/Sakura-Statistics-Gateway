"""Python 3.9+ standard-library example. Use one Client per app per process."""
import datetime as dt
import json
from pathlib import Path
import random
import sqlite3
import threading
import time
import urllib.error
import urllib.request
import uuid

_PROCESS_SESSION_ID = str(uuid.uuid4())
_START_LOCK = threading.Lock()
_STARTED = set()
SHANGHAI = dt.timezone(dt.timedelta(hours=8), "Asia/Shanghai")


class Client:
    def __init__(self, endpoint, app_id, state_dir, *, capacity=10000, batch_size=100,
                 timeout=5, statistics_timezone=SHANGHAI, app_version=None,
                 platform=None, autostart=True, transport=None, clock=None):
        if capacity < 1 or not 1 <= batch_size <= 100:
            raise ValueError("capacity must be positive; batch_size must be 1..100")
        if timeout <= 0:
            raise ValueError("timeout must be positive")
        self.endpoint = endpoint.rstrip("/") + "/api/collect"
        self.app_id = app_id
        self.capacity, self.batch_size, self.timeout = capacity, batch_size, timeout
        self.statistics_timezone = statistics_timezone
        self.app_version, self.platform = app_version, platform
        self._clock = clock or (lambda: dt.datetime.now(dt.timezone.utc))
        self._transport = transport or self._http
        self._lock = threading.RLock()
        self._send_lock = threading.Lock()
        self._stop, self._wake = threading.Event(), threading.Event()
        self._closed = False
        self._db_closed = False
        self._last_active = None
        self._thread = None
        self.rejected_count = 0
        self.overflow_count = 0
        self.last_error = None
        state = Path(state_dir).expanduser()
        state.mkdir(parents=True, exist_ok=True)
        self._db = sqlite3.connect(str(state / "queue.sqlite3"), check_same_thread=False)
        self._db.execute("PRAGMA busy_timeout=5000")
        self._db.execute("PRAGMA journal_mode=WAL")
        self._db.executescript("""
            CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
            CREATE TABLE IF NOT EXISTS queue (
                seq INTEGER PRIMARY KEY AUTOINCREMENT, app_id TEXT NOT NULL,
                event_id TEXT NOT NULL UNIQUE, payload TEXT NOT NULL);
        """)
        with self._db:
            self._db.execute("INSERT OR IGNORE INTO metadata VALUES ('anonymous_id', ?)",
                             (str(uuid.uuid4()),))
        self.anonymous_id = self._db.execute(
            "SELECT value FROM metadata WHERE key='anonymous_id'").fetchone()[0]
        self.session_id = _PROCESS_SESSION_ID
        start_key = (str(state.resolve()), app_id)
        with _START_LOCK:
            if start_key not in _STARTED and self._enqueue("app_start"):
                _STARTED.add(start_key)
        if autostart:
            self._thread = threading.Thread(target=self._run, name="sakura-events", daemon=True)
            self._thread.start()

    def _enqueue(self, event_type, *, now=None, path=None):
        now = now or self._clock()
        if now.tzinfo is None:
            raise ValueError("clock must return timezone-aware datetime")
        event = {
            "schema_version": 1, "event_id": str(uuid.uuid4()), "event_type": event_type,
            "occurred_at": now.astimezone(dt.timezone.utc).isoformat().replace("+00:00", "Z"),
            "anonymous_id": self.anonymous_id, "session_id": self.session_id,
            "sdk_version": "python-example/1.0",
        }
        for key, value in (("app_version", self.app_version), ("platform", self.platform), ("path", path)):
            if value is not None:
                event[key] = value
        with self._lock:
            if self._closed:
                return False
            with self._db:
                count = self._db.execute("SELECT count(*) FROM queue").fetchone()[0]
                if count >= self.capacity:
                    self.overflow_count += 1
                    return False  # Preserve already queued events; drop the new one explicitly.
                self._db.execute("INSERT INTO queue(app_id,event_id,payload) VALUES(?,?,?)",
                                 (self.app_id, event["event_id"], json.dumps(event)))
        self._wake.set()
        return True

    def mark_active(self):
        """Call on real interaction; at most once per 5 min, plus first activity of a new day."""
        now = self._clock()
        day = now.astimezone(self.statistics_timezone).date()
        tick = time.monotonic()
        with self._lock:
            if self._last_active is not None:
                last_day, last_tick = self._last_active
                if day == last_day and tick - last_tick < 300:
                    return False
            if not self._enqueue("active", now=now):
                return False
            self._last_active = (day, tick)
            return True

    @property
    def pending(self):
        with self._lock:
            return self._db.execute("SELECT count(*) FROM queue WHERE app_id=?", (self.app_id,)).fetchone()[0]

    def _http(self, payload):
        request = urllib.request.Request(self.endpoint, data=json.dumps(payload).encode("utf-8"),
                                         headers={"Content-Type": "application/json"}, method="POST")
        with urllib.request.urlopen(request, timeout=self.timeout) as response:
            # Bound response memory. Malformed/oversized responses remain retryable.
            body = response.read(1024 * 1024 + 1)
            if len(body) > 1024 * 1024:
                raise ValueError("collect response too large")
            return json.loads(body)

    def flush_once(self):
        """One synchronous attempt, primarily for testing. Background worker calls this."""
        with self._send_lock:
            with self._lock:
                if self._closed:
                    return False
                rows = self._db.execute(
                    "SELECT event_id,payload FROM queue WHERE app_id=? ORDER BY seq LIMIT ?",
                    (self.app_id, self.batch_size)).fetchall()
            if not rows:
                return True
            try:
                result = self._transport({"app_id": self.app_id,
                                           "events": [json.loads(row[1]) for row in rows]})
                if not isinstance(result, dict):
                    raise ValueError("invalid collect response")
                accepted, duplicates, rejected = (result.get(k) for k in ("accepted", "duplicates", "rejected"))
                if type(accepted) is not int or type(duplicates) is not int or not isinstance(rejected, list):
                    raise ValueError("invalid collect counts")
                ids = {row[0] for row in rows}
                rejected_ids = set()
                for entry in rejected:
                    if not isinstance(entry, dict) or entry.get("event_id") not in ids or not entry.get("code"):
                        raise ValueError("invalid rejected event")
                    rejected_ids.add(entry["event_id"])
                if (accepted < 0 or duplicates < 0 or len(rejected_ids) != len(rejected)
                        or accepted + duplicates + len(rejected) != len(rows)):
                    raise ValueError("incomplete collect response")
                with self._lock, self._db:
                    self._db.executemany("DELETE FROM queue WHERE event_id=?", [(row[0],) for row in rows])
                    self.rejected_count += len(rejected)
                    self.last_error = ("server rejected " + str(rejected)) if rejected else None
                return True
            except Exception as exc:
                # Connection loss, HTTP errors, and invalid acknowledgments keep original payloads.
                self.last_error = f"{type(exc).__name__}: {exc}"
                return False

    def _run(self):
        failures = 0
        try:
            while not self._stop.is_set():
                self._wake.clear()
                if not self.flush_once():
                    failures += 1
                    delay = random.uniform(0.5, min(60, 2 ** min(failures, 6)))
                    # New events must not bypass retry backoff.
                    self._stop.wait(delay)
                    continue
                failures = 0
                with self._lock:
                    if self._closed:
                        break
                    empty = self.pending == 0
                if empty:
                    self._wake.wait(1)
        finally:
            with self._lock:
                if not self._db_closed:
                    self._db.close()
                    self._db_closed = True

    def close(self, wait=0.2):
        """Stop promptly; persisted queue is retried next launch. Never flush on exit."""
        with self._lock:
            if self._closed:
                return
            self._closed = True
        self._stop.set()
        self._wake.set()
        if self._thread:
            self._thread.join(timeout=max(0, wait))
        # An in-flight daemon may still be acknowledging a request. Do not close its connection.
        if not self._thread or not self._thread.is_alive():
            with self._lock:
                if not self._db_closed:
                    self._db.close()
                    self._db_closed = True

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()

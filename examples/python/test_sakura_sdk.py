import copy
import datetime as dt
from pathlib import Path
import tempfile
import threading
import time
import unittest

from sakura_sdk import Client, SHANGHAI


def acknowledge(payload):
    return {"accepted": len(payload["events"]), "duplicates": 0, "rejected": []}


class SDKTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.clients = []
        self.addCleanup(lambda: [client.close() for client in self.clients])

    def client(self, **kwargs):
        client = Client("http://localhost:8080", "test-app", self.temp.name,
                        autostart=False, **kwargs)
        self.clients.append(client)
        return client

    def test_installation_id_persists_and_is_random_per_installation(self):
        first = self.client()
        anonymous_id, session_id = first.anonymous_id, first.session_id
        first.close()
        second = self.client()
        self.assertEqual(anonymous_id, second.anonymous_id)
        self.assertEqual(session_id, second.session_id)
        other = Client("http://localhost", "test-app", Path(self.temp.name) / "other", autostart=False)
        self.addCleanup(other.close)
        self.assertNotEqual(anonymous_id, other.anonymous_id)

    def test_network_retry_reuses_exact_event_after_reopening(self):
        attempts = []

        def uncertain_delivery(payload):
            attempts.append(copy.deepcopy(payload))
            raise TimeoutError("server may have committed before timeout")

        client = self.client(transport=uncertain_delivery)
        self.assertFalse(client.flush_once())
        self.assertEqual(client.pending, 1)
        client.close()

        def duplicate_ack(payload):
            attempts.append(copy.deepcopy(payload))
            return {"accepted": 0, "duplicates": len(payload["events"]), "rejected": []}

        reopened = self.client(transport=duplicate_ack)
        self.assertTrue(reopened.flush_once())
        self.assertEqual(attempts[0], attempts[1])
        self.assertEqual(reopened.pending, 0)

    def test_queue_capacity_preserves_existing_events(self):
        attempts = []
        client = self.client(capacity=2, transport=lambda p: attempts.append(p) or acknowledge(p))
        self.assertTrue(client.mark_active())
        self.assertFalse(client._enqueue("active"))
        self.assertEqual(client.pending, 2)
        self.assertEqual(client.overflow_count, 1)
        self.assertTrue(client.flush_once())
        self.assertEqual([e["event_type"] for e in attempts[0]["events"]], ["app_start", "active"])

    def test_real_activity_coalesces_but_crosses_statistics_midnight(self):
        now = [dt.datetime(2026, 10, 30, 15, 59, 59, tzinfo=dt.timezone.utc)]
        client = self.client(clock=lambda: now[0])
        self.assertEqual(client.pending, 1)  # No synthetic activity on startup.
        self.assertTrue(client.mark_active())
        self.assertFalse(client.mark_active())
        now[0] += dt.timedelta(seconds=2)
        self.assertEqual(now[0].astimezone(SHANGHAI).day, 31)
        self.assertTrue(client.mark_active())
        self.assertEqual(client.pending, 3)

    def test_incomplete_response_keeps_entire_batch(self):
        client = self.client(transport=lambda _: {"accepted": 0, "duplicates": 0, "rejected": []})
        self.assertFalse(client.flush_once())
        self.assertEqual(client.pending, 1)

    def test_explicit_rejection_does_not_poison_queue(self):
        def reject(payload):
            return {"accepted": 0, "duplicates": 0, "rejected": [
                {"event_id": e["event_id"], "code": "invalid_event", "message": "invalid"}
                for e in payload["events"]]}
        client = self.client(transport=reject)
        self.assertTrue(client.flush_once())
        self.assertEqual(client.pending, 0)
        self.assertEqual(client.rejected_count, 1)
        self.assertIn("invalid_event", client.last_error)

    def test_close_does_not_wait_for_network(self):
        entered, release = threading.Event(), threading.Event()

        def slow_transport(payload):
            entered.set()
            release.wait(2)
            return acknowledge(payload)

        client = Client("http://localhost", "slow", Path(self.temp.name) / "slow", transport=slow_transport)
        self.assertTrue(entered.wait(1))
        started = time.monotonic()
        client.close(wait=0.01)
        self.assertLess(time.monotonic() - started, 0.5)
        release.set()
        client._thread.join(2)
        self.assertFalse(client._thread.is_alive())
        self.assertTrue(client._db_closed)


if __name__ == "__main__":
    unittest.main()

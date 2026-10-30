"""Unit-test safety gates without Docker or a live database."""
import contextlib
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import backup
import restore


class BackupTests(unittest.TestCase):
    def test_existing_output_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "existing.dump"
            output.write_bytes(b"original")
            with patch.object(sys, "argv", ["backup.py", str(output)]), patch.object(
                    backup.subprocess, "run") as run, contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(backup.main(), 1)
                run.assert_not_called()
            self.assertEqual(output.read_bytes(), b"original")

    def test_failed_dump_removes_partial_output(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "failed.dump"
            def fail(command, **kwargs):
                kwargs["stdout"].write(b"partial")
                raise subprocess.CalledProcessError(1, command)
            with patch.object(sys, "argv", ["backup.py", str(output)]), patch.object(
                    backup.subprocess, "run", side_effect=fail), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(backup.main(), 1)
            self.assertFalse(output.exists())


class RestoreTests(unittest.TestCase):
    def invoke(self, responses):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "backup.dump"
            source.write_bytes(b"mock archive")
            with patch.object(sys, "argv", ["restore.py", str(source), "--confirm-empty-database", "sakura"]), patch.object(
                    restore.subprocess, "run", side_effect=responses) as run, contextlib.redirect_stderr(io.StringIO()):
                result = restore.main()
                return result, run.call_args_list

    def test_running_app_prevents_restore(self):
        result, calls = self.invoke([subprocess.CompletedProcess([], 0, stdout="container-id\n")])
        self.assertEqual(result, 1)
        self.assertEqual(len(calls), 1)

    def test_nonempty_database_prevents_restore(self):
        result, calls = self.invoke([
            subprocess.CompletedProcess([], 0, stdout=""),
            subprocess.CompletedProcess([], 0),
            subprocess.CompletedProcess([], 0, stdout="7\n"),
        ])
        self.assertEqual(result, 1)
        self.assertEqual(len(calls), 3)
        self.assertTrue(all("pg_restore" not in " ".join(call.args[0]) for call in calls))

    def test_empty_database_uses_transaction_without_clean(self):
        with contextlib.redirect_stdout(io.StringIO()):
            result, calls = self.invoke([
                subprocess.CompletedProcess([], 0, stdout=""),
                subprocess.CompletedProcess([], 0),
                subprocess.CompletedProcess([], 0, stdout="0\n"),
                subprocess.CompletedProcess([], 0),
            ])
        self.assertEqual(result, 0)
        command = " ".join(calls[-1].args[0])
        self.assertIn("--single-transaction", command)
        self.assertIn("--exit-on-error", command)
        self.assertNotIn("--clean", command)


if __name__ == "__main__":
    unittest.main()

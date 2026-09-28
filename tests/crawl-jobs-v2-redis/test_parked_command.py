"""Bounded local subprocess controls; these are not Docker-worker death evidence."""
import os
import signal
import sys
import time
import unittest

import harness as h
import parked_command as parked


class ParkedCommandTests(unittest.TestCase):
    def start(self, source, timeout=5):
        return parked.start([sys.executable, "-B", "-c", source], b"{}\n", timeout)

    def test_acknowledged_process_stays_alive_until_actual_kill(self):
        handle, result = self.start("import sys,signal; sys.stdin.buffer.read(); print('{\"ready\":true}',flush=True); signal.pause()")
        try:
            self.assertEqual(result, {"ready": True})
            handle.require_waiting()
            os.killpg(handle.process.pid, signal.SIGKILL)
            self.assertEqual(handle.finish(2), -signal.SIGKILL)
            self.assertTrue(handle.finished)
        finally:
            handle.abort()

    def test_partial_malformed_and_duplicate_acknowledgments_fail_closed(self):
        for source in (
            "import sys; sys.stdin.buffer.read(); print('{bad-json',flush=True)",
            "import sys,signal; sys.stdin.buffer.read(); print('{\"x\":1}\\n{\"x\":2}',flush=True); signal.pause()",
            "import sys; sys.stdin.buffer.read(); sys.stdout.write('{\"partial\":'); sys.stdout.flush()"):
            with self.subTest(source=source), self.assertRaises((h.InvalidArtifact, parked.ParkedCommandError)):
                self.start(source)

    def test_timeout_and_output_bounds_terminate_owned_command(self):
        started = time.monotonic()
        with self.assertRaises(parked.ParkedCommandError):
            self.start("import sys,time; sys.stdin.buffer.read(); time.sleep(30)", 0.2)
        self.assertLess(time.monotonic() - started, 6)
        with self.assertRaises((h.InvalidArtifact, parked.ParkedCommandError)):
            self.start("import sys,time; sys.stdin.buffer.read(); sys.stdout.write('x'*2097153); sys.stdout.flush(); time.sleep(30)")

    def test_extra_output_after_acknowledgment_invalidates_finish(self):
        handle, _ = self.start("import sys,time; sys.stdin.buffer.read(); print('{\"ready\":true}',flush=True); time.sleep(.2); print('{\"extra\":true}',flush=True); time.sleep(30)")
        try:
            with self.assertRaises(h.InvalidArtifact):
                handle.finish(2)
            self.assertTrue(handle.finished)
        finally:
            handle.abort()


if __name__ == "__main__":
    unittest.main()

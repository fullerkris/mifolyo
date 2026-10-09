"""Bounded local Python pipe fixtures only; never Docker or native actors."""
import sys
import time
import unittest
from unittest.mock import patch

import contracts as c
from streams import Deadline, FrameBuffer, ProcessStream, WireBudget
from runtime_admission import PHASES
from test_runtime_admission import frames

D = c.sha(b"synthetic-stream")


def binding(role="target"):
    return {"case_id": "P1.pre.1.1", "invocation_sha256": D, "container_id": D,
        "image": "sha256:" + D, "role": role}


def process(script, role="target", seconds=2, before_input=None):
    # Fixed synthetic native preamble, including the fresh scope nonce. The
    # observer waits for A before its old ARMED/HELD test body can execute.
    prefix = 'sys.stdin.readline(); sys.stdin.readline(); '
    prefix += ''.join('sys.stdout.buffer.write(' + repr(raw) + '); sys.stdout.flush(); ' for raw in frames(role, bound=binding(role)))
    if role == "observer":
        prefix += 'print(\'{"phase":"ADMISSION_READY","version":1}\',flush=True); assert sys.stdin.read(1)=="A"; '
    script = script.replace('sys.stdin.readline(); ', prefix, 1)
    return ProcessStream([sys.executable, "-I", "-B", "-c", script], binding(role), WireBudget(), Deadline(seconds), before_input=before_input)


def select(stream):
    stream.send_case()
    for phase in PHASES:
        stream.receive(phase)
    if stream.binding["role"] == "observer":
        stream.receive("ADMISSION_READY")
        stream.arm_observer()


class FrameTests(unittest.TestCase):
    def test_native_identity_frame_survives_every_fragment_boundary(self):
        raw = frames()[0]
        for split in range(len(raw) + 1):
            parser = FrameBuffer(WireBudget())
            self.assertEqual(parser.feed(raw[:split]) + parser.feed(raw[split:]), [raw])
            parser.eof()

    def test_every_split_preserves_exact_frame_and_shared_budget(self):
        raw = c.canonical({"phase": "READY", "version": 1})
        for split in range(len(raw) + 1):
            budget = WireBudget()
            parser = FrameBuffer(budget)
            result = parser.feed(raw[:split]) + parser.feed(raw[split:])
            self.assertEqual(result, [raw])
            parser.eof()
            self.assertEqual((budget.bytes, budget.frames), (len(raw), 1))
        budget = WireBudget()
        for _ in range(64):
            FrameBuffer(budget).feed(raw)
        with self.assertRaisesRegex(c.Invalid, "STREAM_FRAMES"):
            FrameBuffer(budget).feed(raw)

    def test_malformed_truncated_overlong_and_noncanonical_frames_fail(self):
        for raw in (b'{"a":1,"a":2}\n', b'{"a":1.0}\n', b'{"a": NaN}\n', b"x" * 1025,
                    b'{"a":1}\r\n', b' {"a":1}\n', b'[]\n', b'{"a":"\xff"}\n'):
            with self.subTest(raw=raw[:20]), self.assertRaises(c.Invalid):
                FrameBuffer(WireBudget()).feed(raw)
        parser = FrameBuffer(WireBudget())
        parser.feed(b'{"phase":"READY"')
        with self.assertRaisesRegex(c.Invalid, "TRUNCATED_FRAME"):
            parser.eof()
        budget = WireBudget()
        budget.consume(65536)
        with self.assertRaises(c.Invalid):
            budget.consume(1)


class ProcessStreamTests(unittest.TestCase):
    def test_slow_before_input_cannot_write_after_stream_deadline(self):
        delayed = [False]
        stream_holder = []
        def before_input():
            if delayed[0]:
                time.sleep(max(0, stream_holder[0].deadline.end - time.monotonic()) + 0.02)
        script = 'import sys; sys.stdin.readline(); print(\'{"phase":"READY","version":1}\',flush=True); sys.stdin.read(1)'
        with process(script, seconds=0.75, before_input=before_input) as stream:
            stream_holder.append(stream)
            select(stream)
            stream.receive("READY")
            delayed[0] = True
            with patch("streams.os.write") as writer, self.assertRaisesRegex(c.Invalid, "DEADLINE"):
                stream.start_target()
            writer.assert_not_called()

    def test_eof_and_extra_output_cannot_count_as_live_admission(self):
        for tail in ('', 'print(\'{"phase":"FINISHED","version":1}\',flush=True); sys.stdin.read(1)'):
            script = 'import sys; sys.stdin.readline(); print(\'{"phase":"READY","version":1}\',flush=True); ' + tail
            with process(script) as stream:
                select(stream)
                stream.receive("READY")
                while not (stream.stdout_eof or stream.frames or stream.decoder.partial):
                    stream._pump(min(stream.deadline.remaining(), 0.01))
                with self.assertRaisesRegex(c.Invalid, "NATIVE_ADMISSION_TRANSPORT"):
                    stream.require_admission_ready()

    def test_observer_cannot_arm_before_all_admission_frames(self):
        script = 'import sys; sys.stdin.readline(); print(\'{"phase":"ARMED","version":1}\',flush=True)'
        with process(script, "observer") as stream:
            stream.send_case()
            for phase in PHASES:
                with self.assertRaisesRegex(c.Invalid, "EARLY_ARM"):
                    stream.arm_observer()
                stream.receive(phase)
            stream.receive("ADMISSION_READY")
            stream.arm_observer()
            stream.receive("ARMED")

    def test_target_protocol_and_eof_do_not_claim_remote_exit(self):
        script = 'import sys; sys.stdin.readline(); print(\'{"phase":"READY","version":1}\',flush=True); assert sys.stdin.read(1)=="G"; print(\'{"phase":"FINISHED","version":1}\',flush=True)'
        with process(script) as stream:
            select(stream)
            stream.receive("READY")
            stream.start_target()
            stream.receive("FINISHED")
            result = stream.finish_transport()
            self.assertTrue(result["stdout_closed"])
            self.assertFalse(result["remote_exit_proven"])

    def test_observer_requires_held_before_resume_and_closes_once(self):
        script = 'import sys; sys.stdin.readline(); print(\'{"phase":"ARMED","version":1}\',flush=True); print(\'{"events":2,"ordinal":1,"phase":"HELD","read_bytes":32,"version":1}\',flush=True); assert sys.stdin.read(1)=="R"; print(\'{"phase":"CONTINUATION_DISPATCHED","version":1}\',flush=True)'
        with process(script, "observer") as stream:
            with self.assertRaisesRegex(c.Invalid, "EARLY_RESUME"):
                stream.resume_observer()
            select(stream)
            stream.receive("ARMED")
            stream.receive("HELD")
            stream.resume_observer()
            stream.receive("CONTINUATION_DISPATCHED")
            stream.finish_transport()
            stream.close()

    def test_stderr_and_private_canaries_never_enter_diagnostics(self):
        script = 'import sys; sys.stdin.readline(); sys.stderr.write("private-canary-token"); sys.stderr.flush()'
        with process(script) as stream:
            stream.send_case()
            with self.assertRaises(c.Invalid) as caught:
                for phase in (*PHASES, "READY"):
                    stream.receive(phase)
            self.assertNotIn("private-canary", str(caught.exception))
            self.assertTrue(stream.closed)

    def test_early_finish_duplicate_and_unknown_output_are_rejected(self):
        for line in ('{"phase":"FINISHED","version":1}', '{"phase":"READY","private":"canary","version":1}',
                     '{"cleanup_required":true,"reason":"ptrace_denied","status":"INVALID","version":1}'):
            script = 'import sys; sys.stdin.readline(); print(' + repr(line) + ',flush=True)'
            with process(script) as stream:
                with self.assertRaises(c.Invalid):
                    select(stream)
                    stream.receive("READY")

    def test_timeout_covers_silent_child_and_reaps_the_owned_group(self):
        script = 'import sys,time; sys.stdin.readline(); time.sleep(10)'
        with process(script, seconds=0.5) as stream:
            select(stream)
            with self.assertRaises(c.Invalid):
                stream.receive("READY")
            self.assertTrue(stream.closed)
            self.assertIsNotNone(stream._process.returncode)

    def test_input_authorizer_is_rechecked_before_target_start(self):
        permitted = [True]
        def before_input():
            c.require(permitted[0], "EXPIRED")
        script = 'import sys; sys.stdin.readline(); print(\'{"phase":"READY","version":1}\',flush=True); sys.stdin.read(1)'
        with process(script, before_input=before_input) as stream:
            select(stream)
            stream.receive("READY")
            permitted[0] = False
            with self.assertRaisesRegex(c.Invalid, "EXPIRED"):
                stream.start_target()
            self.assertTrue(stream.closed)

    def test_partial_output_cannot_be_relabelled_as_post_dispatch(self):
        target = 'import sys; sys.stdin.readline(); print(\'{"phase":"READY","version":1}\',flush=True); sys.stdout.write(\'{"phase":"FINISHED","version":1}\'); sys.stdout.flush(); sys.stdin.read(1); print("",flush=True)'
        observer = 'import sys; sys.stdin.readline(); print(\'{"phase":"ARMED","version":1}\',flush=True); print(\'{"events":2,"ordinal":1,"phase":"HELD","read_bytes":32,"version":1}\',flush=True); sys.stdout.write(\'{"phase":"CONTINUATION_DISPATCHED","version":1}\'); sys.stdout.flush(); sys.stdin.read(1); print("",flush=True)'
        for role, script, phases in (("target", target, ("READY",)), ("observer", observer, ("ARMED", "HELD"))):
            with process(script, role) as stream:
                select(stream)
                for phase in phases:
                    stream.receive(phase)
                while not stream.decoder.partial:
                    stream._pump(min(stream.deadline.remaining(), 0.01))
                with self.assertRaisesRegex(c.Invalid, "EARLY_ACTOR_OUTPUT"):
                    (stream.start_target if role == "target" else stream.resume_observer)()


if __name__ == "__main__":
    unittest.main()

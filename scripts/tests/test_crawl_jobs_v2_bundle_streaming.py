"""Independent framing equality for bounded-memory bundle hashing."""
import hashlib
from pathlib import Path
import runpy
import struct
import unittest

generator = runpy.run_path(str(Path(__file__).resolve().parents[1] / "generate-crawl-jobs-v2-bundle.py"))


def frame(value):
    raw = value.encode() if isinstance(value, str) else value
    return struct.pack(">Q", len(raw)) + raw


def record(fields):
    return struct.pack(">Q", len(fields)) + b"".join(frame(key) + frame(value) for key, value in fields)


class StreamingBundleTests(unittest.TestCase):
    def test_streamed_contract_matches_literal_framing(self):
        for document, sources in ((b"protocol\n", []), (b"unicode \xc3\xa9\n", [("a.lua", b"return 1\n")]),
                                  (b"doc\n", [("a.lua", b"x" * 900000), ("b.lua", b"\x00binary\n")])):
            expected = frame("mifolyo:crawl-contract:v2")
            for label, rows in (("document", [[("document_bytes", document)]]),
                                ("lua", [[("source_name", name), ("source_bytes", raw)] for name, raw in sources])):
                expected += frame(label) + struct.pack(">Q", len(rows)) + b"".join(frame(record(row)) for row in rows)
            self.assertEqual(generator["contract_digest"](document, sources), hashlib.sha256(expected).hexdigest())


if __name__ == "__main__":
    unittest.main()

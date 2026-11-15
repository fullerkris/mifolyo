#!/usr/bin/env python3
"""Inspect fixed local Linux/Docker prerequisites without starting C0 actors."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tests/crawl-jobs-v2-observer"))
from host_preflight import main

if __name__ == "__main__":
    raise SystemExit(main())

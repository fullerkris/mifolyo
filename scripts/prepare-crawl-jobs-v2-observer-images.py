#!/usr/bin/env python3
"""Build and statically verify one pinned C0 runtime image set."""
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tests/crawl-jobs-v2-observer"))
from image_build import main

if __name__ == "__main__":
    raise SystemExit(main())

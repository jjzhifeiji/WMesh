#!/usr/bin/env python3
"""拦住 model/、domain/ 引用 android.* 或 Compose。"""

from __future__ import annotations

import os
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
SRC = os.path.join(ROOT, "app", "src", "main", "kotlin", "com", "gbndt", "shijiaoqi")
TARGETS = (os.path.join(SRC, "model"), os.path.join(SRC, "domain"))
BANNED = ("import android.", "import androidx.compose")


def offenders() -> list[str]:
    hits: list[str] = []
    for root in TARGETS:
        if not os.path.isdir(root):
            hits.append(f"missing {os.path.relpath(root, ROOT)}")
            continue
        for dirpath, _, files in os.walk(root):
            for name in files:
                if not name.endswith(".kt"):
                    continue
                path = os.path.join(dirpath, name)
                with open(path, encoding="utf-8") as f:
                    for i, line in enumerate(f, 1):
                        stripped = line.strip()
                        if any(stripped.startswith(b) for b in BANNED):
                            rel = os.path.relpath(path, ROOT)
                            hits.append(f"{rel}:{i}: {stripped}")
    return hits


def main() -> int:
    bad = offenders()
    if bad:
        print("model/domain must not import android.* or androidx.compose.*")
        print("\n".join(bad))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

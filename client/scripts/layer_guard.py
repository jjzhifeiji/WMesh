#!/usr/bin/env python3
"""拦住 model/、domain/、geom/ 引用 android.* 或 Compose；geom 不依赖本应用其它包。"""

from __future__ import annotations

import os
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
SRC = os.path.join(ROOT, "app", "src", "main", "kotlin", "com", "gbndt", "shijiaoqi")
TARGETS = (os.path.join(SRC, "model"), os.path.join(SRC, "domain"), os.path.join(SRC, "geom"))
BANNED = ("import android.", "import androidx.compose")
GEOM = os.path.join(SRC, "geom")
GEOM_OK = "import com.gbndt.shijiaoqi.geom"


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
                in_geom = os.path.isdir(GEOM) and os.path.commonpath([path, GEOM]) == GEOM
                with open(path, encoding="utf-8") as f:
                    for i, line in enumerate(f, 1):
                        stripped = line.strip()
                        rel = os.path.relpath(path, ROOT)
                        if any(stripped.startswith(b) for b in BANNED):
                            hits.append(f"{rel}:{i}: {stripped}")
                        if in_geom and stripped.startswith("import com.gbndt.shijiaoqi.") and not stripped.startswith(GEOM_OK):
                            hits.append(f"{rel}:{i}: geom must not import app packages: {stripped}")
    return hits


def main() -> int:
    bad = offenders()
    if bad:
        print("layer_guard failed")
        print("\n".join(bad))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

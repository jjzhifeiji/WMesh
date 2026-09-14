#!/usr/bin/env python3
"""WMesh 治理门禁：拦住缺注释和分层回退。对齐 HTGlobal 的 orm/service/sql Guard。"""

from __future__ import annotations

import os
import re
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
SIDES = (
    os.path.join(ROOT, "global", "server"),
    os.path.join(ROOT, "factory", "server"),
)

FUNC = re.compile(r"^func (\([^)]+\) )?([A-Za-z_][A-Za-z0-9]*)\(")
ERR_NEW = re.compile(r"Err[A-Za-z0-9]+\s*=\s*errors\.New\(")
SKIP_FUNCS = {"TableName"}


def go_files(root: str, tests: bool) -> list[str]:
    out = []
    for dirpath, _, files in os.walk(root):
        if "/vendor/" in dirpath.replace("\\", "/"):
            continue
        for name in files:
            if not name.endswith(".go"):
                continue
            is_test = name.endswith("_test.go")
            if tests != is_test:
                continue
            out.append(os.path.join(dirpath, name))
    return out


def rel(path: str) -> str:
    return os.path.relpath(path, ROOT)


def comment_offenders() -> list[str]:
    bad = []
    for side in SIDES:
        for path in go_files(side, tests=False):
            with open(path, encoding="utf-8") as f:
                lines = f.read().splitlines()
            for i, line in enumerate(lines):
                m = FUNC.match(line)
                if not m or m.group(2) in SKIP_FUNCS:
                    continue
                prev = i - 1
                while prev >= 0 and lines[prev].strip() == "":
                    prev -= 1
                if prev < 0 or not lines[prev].lstrip().startswith("//"):
                    bad.append(f"{rel(path)}:{i + 1} {m.group(2)} 缺正上方注释")
            for i, line in enumerate(lines):
                if not ERR_NEW.search(line):
                    continue
                if "//" in line:
                    continue
                prev = i - 1
                while prev >= 0 and lines[prev].strip() == "":
                    prev -= 1
                if prev < 0 or not lines[prev].lstrip().startswith("//"):
                    bad.append(f"{rel(path)}:{i + 1} 业务错误缺注释")
    return bad


def layer_offenders() -> list[str]:
    bad = []
    for path in go_files(os.path.join(ROOT, "global", "server"), tests=False):
        text = open(path, encoding="utf-8").read()
        if "wmesh/factory" in text:
            bad.append(f"{rel(path)} WAN 禁止 import wmesh/factory")
        if "AutoMigrate" in text:
            bad.append(f"{rel(path)} 禁止 GORM AutoMigrate")
    for path in go_files(os.path.join(ROOT, "factory", "server"), tests=False):
        text = open(path, encoding="utf-8").read()
        if "wmesh/global" in text:
            bad.append(f"{rel(path)} 厂内禁止 import wmesh/global")
        if "AutoMigrate" in text:
            bad.append(f"{rel(path)} 禁止 GORM AutoMigrate")
    for side in SIDES:
        for path in go_files(side, tests=False):
            p = path.replace("\\", "/")
            text = open(path, encoding="utf-8").read()
            if "/internal/service/" in p and "gorm.io" in text:
                bad.append(f"{rel(path)} Service 禁止直连 GORM")
            if "/internal/httpapi/" in p and re.search(r'"wmesh/(global|factory)/internal/store"', text):
                bad.append(f"{rel(path)} HTTP 禁止绕过 Service 直连 Store")
    return bad


def main() -> int:
    offenders = comment_offenders() + layer_offenders()
    if not offenders:
        print("style_guard ok")
        return 0
    top = offenders[:50]
    print(f"style_guard 失败（显示前 {len(top)} 条，共 {len(offenders)} 条）")
    print("\n".join(top))
    return 1


if __name__ == "__main__":
    sys.exit(main())

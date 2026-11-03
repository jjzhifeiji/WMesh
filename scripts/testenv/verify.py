#!/usr/bin/env python3
"""核对初始状态是否符合 seed.json，并写基线 baseline.local.json（测试方案 §1.5 的基线）。

    python3 scripts/testenv/verify.py
"""

from __future__ import annotations

import json
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from seed import ApiError, HERE, ROOT, STATE, call, env_value, files  # noqa: E402

BASELINE = os.path.join(HERE, "baseline.local.json")


def psql(db: str, sql: str) -> str:
    out = subprocess.run(
        ["docker", "compose", "exec", "-T", "db", "sh", "-c", f'psql -U "$POSTGRES_USER" -d {db} -Atc "{sql}"'],
        cwd=os.path.join(ROOT, "factory"), capture_output=True, text=True, check=True,
    )
    return out.stdout.strip()


def main():
    cfg = json.load(open(os.path.join(HERE, "seed.json"), encoding="utf-8"))
    state = json.load(open(STATE, encoding="utf-8"))
    W, F = cfg["wan"]["url"], cfg["factory_url"]
    fails: list[str] = []
    base: dict = {"wan": {}, "factories": {}}

    def check(ok: bool, msg: str):
        print(("  ✓ " if ok else "  ✗ ") + msg)
        if not ok:
            fails.append(msg)

    print("== WAN")
    wt = call(W, "POST", "/v1/login", {"loginName": cfg["wan"]["login"],
                                       "password": env_value(cfg["wan"]["password_env_file"], cfg["wan"]["password_env_key"])})["token"]
    directory = call(W, "GET", "/v1/directory", token=wt)
    facs = directory.get("factories", directory) if isinstance(directory, dict) else directory
    check(len(facs) == len(cfg["factories"]), f"工厂 {len(facs)} 家")
    clients = call(W, "GET", "/v1/clients", token=wt) or []
    check(len(clients) == len(cfg["devices"]), f"设备 {len(clients)} 台")
    plat = [a for a in call(W, "GET", "/v1/assets", token=wt) or [] if a.get("kind") == "process"]
    want_plat = len(files(os.path.join(ROOT, cfg["data_root"]), os.path.join("processes", cfg["platform"]["processes"])))
    check(len(plat) == want_plat, f"平台工艺 {len(plat)} 条（期望 {want_plat}）")
    check(all(a.get("status") == "available" for a in plat), "平台工艺全部可用")
    secret = sum(1 for a in plat if not a.get("copyable"))
    check(secret > 0, f"不可复制平台工艺 {secret} 条")
    base["wan"] = {"factories": len(facs), "clients": len(clients), "platformProcesses": len(plat)}

    for fc in cfg["factories"]:
        if not fc["claim"]:
            continue
        st = state["factories"][fc["key"]]
        fid, P = st["id"], f"/v1/factories/{st['id']}"
        print(f"== {fc['name']}")
        t = call(F, "POST", f"{P}/login", {"loginName": fc["sa"]["login"], "password": fc["sa"]["password"]})["token"]
        assets = call(F, "GET", f"{P}/assets", token=t) or []
        by = lambda lvl, kind: [a for a in assets if a.get("level") == lvl and a.get("kind") == kind]
        check(len(by("platform", "process")) == want_plat, f"平台副本 {len(by('platform', 'process'))} 条")
        check(len(by("factory", "process")) >= len(fc.get("factory_processes", [])), f"厂级工艺 {len(by('factory', 'process'))} 条")
        check(len(by("factory", "project")) == len(fc.get("factory_projects", [])), f"厂级工程 {len(by('factory', 'project'))} 条")
        check(all(a.get("status") == "available" for a in by("factory", "process") + by("factory", "project")), "厂级资产全部可用")
        devs = call(F, "GET", f"{P}/clients", token=t) or []
        want_dev = sum(1 for d in cfg["devices"] if d["factory"] == fc["key"])
        check(len(devs) == want_dev, f"设备 {len(devs)} 台")
        db = "wmesh_fac_" + fid.replace("-", "")
        enc = psql(db, "select count(*) from assets where substring(content from 1 for 3) <> convert_to('WM2', 'UTF8')")
        check(enc == "0", f"厂库正文全是信封（非信封 {enc} 条）")
        facts = int(psql(db, "select count(*) from weld_facts") or 0)
        base["factories"][fc["key"]] = {
            "id": fid, "people": len(st.get("people", {})) + 1, "assets": len(assets),
            "devices": len(devs), "weldFacts": facts,
        }
        print(f"  · 焊次 {facts} 条")

    with open(BASELINE, "w", encoding="utf-8") as f:
        json.dump(base, f, ensure_ascii=False, indent=2)
    print(f"\n基线写到 {os.path.relpath(BASELINE, ROOT)}")
    if fails:
        sys.exit(f"{len(fails)} 项不符")
    print("初始状态核对通过")


if __name__ == "__main__":
    try:
        main()
    except ApiError as e:
        sys.exit(f"接口失败：{e}")

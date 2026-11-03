#!/usr/bin/env python3
"""按 seed.json 往刚重置的本机 WAN 和厂端灌初始数据，全部走正式接口。

厂库正文是加密信封，绕过接口直接写库灌不进去；走接口顺带把建厂、认领、导入、
发布这条链路冒烟一遍。只在干净状态下跑：发现 WAN 名录里已有工厂就拒绝。

    python3 scripts/testenv/seed.py                 # 灌基础数据
    python3 scripts/testenv/seed.py --with-history  # 再按 history 生成过去 30 天的焊次
"""

from __future__ import annotations

import argparse
import fnmatch
import json
import os
import random
import sys
import time
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timedelta, timezone

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
HERE = os.path.dirname(os.path.abspath(__file__))
STATE = os.path.join(HERE, "state.local.json")  # 灌完记下各身份，verify.py 读


class ApiError(RuntimeError):
    pass


def call(base: str, method: str, path: str, body=None, token: str | None = None, ok=(200, 201, 204)):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            raw = r.read()
            status = r.status
    except urllib.error.HTTPError as e:
        raw, status = e.read(), e.code
    if status not in ok:
        raise ApiError(f"{method} {path} -> {status} {raw[:400].decode('utf-8', 'replace')}")
    return json.loads(raw) if raw else None


def step(msg: str):
    print(f"\n== {msg}", flush=True)


def env_value(path: str, key: str) -> str:
    with open(os.path.join(ROOT, path), encoding="utf-8") as f:
        for line in f:
            if line.startswith(key + "="):
                return line.split("=", 1)[1].strip()
    raise SystemExit(f"{path} 里没有 {key}")


def files(root: str, pattern: str) -> list[str]:
    out = []
    for dirpath, _, names in os.walk(root):
        for n in names:
            rel = os.path.relpath(os.path.join(dirpath, n), root)
            if fnmatch.fnmatch(rel, pattern):
                out.append(rel)
    return sorted(out)


def read(root: str, rel: str) -> str:
    with open(os.path.join(root, rel), encoding="utf-8") as f:
        return f.read()


def remap_paths(content: str, remap: dict[str, str]) -> str:
    """把工程里的旧 processPath 换成种子里真实存在的工艺路径。"""
    def walk(o):
        if isinstance(o, dict):
            return {k: (remap.get(v, v) if k == "processPath" and isinstance(v, str) else walk(v)) for k, v in o.items()}
        if isinstance(o, list):
            return [walk(v) for v in o]
        return o
    return json.dumps(walk(json.loads(content)), ensure_ascii=False)


def fill_empty(content: str, fill: dict) -> str:
    """原始多层工程有的层没配工艺：打底层空的配 fill.base，各层空的按 fill.passes 依次配。"""
    if not fill:
        return content
    paths = json.loads(content)
    for w in paths:
        base = w.get("basePath")
        if isinstance(base, dict) and not base.get("processPath") and fill.get("base"):
            base["processPath"] = fill["base"]
        for i, ps in enumerate(w.get("passes") or []):
            if not ps.get("processPath") and not ps.get("process") and fill.get("passes"):
                ps["processPath"] = fill["passes"][i % len(fill["passes"])]
    return json.dumps(paths, ensure_ascii=False)


def referenced(content: str) -> set[str]:
    acc: set[str] = set()

    def walk(o):
        if isinstance(o, dict):
            for k, v in o.items():
                if k == "processPath" and isinstance(v, str) and v:
                    acc.add(v)
                else:
                    walk(v)
        elif isinstance(o, list):
            for v in o:
                walk(v)
    walk(json.loads(content))
    return acc


def legacy_batch(root: str, processes: list[str], projects: list[dict], level: str | None = None) -> dict:
    """组一批旧文件导入：工艺和工程放同一批，引用才能解开。工艺在 root/processes，工程在 root/projects。"""
    procs = {p: read(os.path.join(root, "processes"), p) for p in processes}
    projs = []
    for pj in projects:
        content = remap_paths(read(os.path.join(root, "projects"), pj["path"]), pj.get("remap", {}))
        content = fill_empty(content, pj.get("fill", {}))
        missing = referenced(content) - set(procs)
        if missing:
            raise SystemExit(f"{pj['path']} 引用了不在本批的工艺：{sorted(missing)}")
        # 工程名取种子里的名字；路径保留模式目录，导入按 single_layer / multi_layer 认作业类型
        mode = pj["path"].split("/", 1)[0]
        projs.append({"path": f"{mode}/{pj['name']}/{os.path.basename(pj['path'])}", "name": pj["name"], "content": content})
    # 同名不同目录的（如两组立焊里都有 2H3-4mm）按路径另起名字，不当重复跳过
    batch = {"processes": [{"path": p, "content": c} for p, c in procs.items()], "projects": projs, "rename": True}
    if level:
        batch["level"] = level
    return batch


def check_import(res: dict, want_proc: int, want_proj: int, label: str):
    rej = res.get("rejected") or []
    got_p, got_j = len(res.get("processes") or []), len(res.get("projects") or [])
    print(f"   {label}：工艺 {got_p}、工程 {got_j}、跳过 {len(res.get('skipped') or [])}、拒绝 {len(rej)}")
    for r in rej:
        print("   拒绝：", r)
    # 多层工程各层内嵌的参数会被提成独立工艺，所以工艺只要求不少于文件数
    if rej or got_p < want_proc or got_j != want_proj:
        raise SystemExit(f"{label} 导入数量不对，期望工艺至少 {want_proc}、工程 {want_proj}")


def publish_all(base: str, prefix: str, token: str, assets: list[dict]):
    for a in assets:
        if a.get("status") == "available":
            continue
        call(base, "POST", f"{prefix}/assets/{a['id']}/publish", {"expected": a["revision"]}, token)




def wait_until(fn, what: str, timeout: float = 60.0):
    end = time.time() + timeout
    last = None
    while time.time() < end:
        try:
            if fn():
                return
        except ApiError as e:
            last = e
        time.sleep(1)
    raise SystemExit(f"等 {what} 超时 {last or ''}")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--seed", default=os.path.join(HERE, "seed.json"))
    ap.add_argument("--with-history", action="store_true")
    a = ap.parse_args()
    cfg = json.load(open(a.seed, encoding="utf-8"))
    root = os.path.join(ROOT, cfg["data_root"])
    W, F = cfg["wan"]["url"], cfg["factory_url"]
    state: dict = {"factories": {}, "devices": {}}

    step("WAN 登录")
    wpw = env_value(cfg["wan"]["password_env_file"], cfg["wan"]["password_env_key"])
    wt = call(W, "POST", "/v1/login", {"loginName": cfg["wan"]["login"], "password": wpw})["token"]
    directory = call(W, "GET", "/v1/directory", token=wt) or []
    if (directory.get("factories") if isinstance(directory, dict) else directory):
        raise SystemExit("WAN 名录里已有工厂，不是干净状态；先跑 reset.sh")

    step("建厂、认领")
    for fc in cfg["factories"]:
        res = call(W, "POST", "/v1/factories", {"name": fc["name"], "saLogin": fc["sa"]["login"], "saDisplay": fc["sa"]["display"]}, wt)
        code = res.get("enrollmentToken", "")  # 一次性建厂码
        fid = res["factory"]["id"]
        print(f"   {fc['name']} id={fid}")
        st = {"id": fid, "name": fc["name"], "claimed": False}
        if fc["claim"]:
            if not code:
                raise SystemExit(f"建厂回包里找不到建厂码：{res}")
            claimed = call(F, "POST", "/v1/site/claim", {"enrollmentCode": code, "password": fc["sa"]["password"]})
            st["id"], st["claimed"] = claimed["factoryId"], True
        state["factories"][fc["key"]] = st

    step("登记设备")
    for d in cfg["devices"]:
        body = {"name": d["name"], "deviceSerial": d["serial"]}
        if d["factory"]:
            body["factoryId"] = state["factories"][d["factory"]]["id"]
        res = call(W, "POST", "/v1/clients", body, wt)
        state["devices"][d["serial"]] = {"id": res["id"], "shortCode": res.get("shortCode"), "factory": d["factory"]}
        print(f"   {d['name']} {d['serial']} -> {d['factory'] or '未分配'} {res.get('shortCode')}")

    step("平台标准工艺库")
    plat = cfg["platform"]
    procs = files(root, os.path.join("processes", plat["processes"]))
    procs = [os.path.relpath(p, "processes") for p in procs]
    res = call(W, "POST", "/v1/legacy-import", legacy_batch(root, procs, []), wt)
    check_import(res, len(procs), 0, "平台工艺")
    publish_all(W, "/v1", wt, res["processes"])
    secret = set(os.path.relpath(p, "processes") for p in files(root, os.path.join("processes", plat["non_copyable"])))
    names = {os.path.splitext(os.path.basename(p))[0] for p in secret}
    for asset in call(W, "GET", "/v1/assets", token=wt) or []:
        if asset.get("kind") != "process":
            continue
        want = asset["name"] not in names
        if asset.get("copyable") != want:
            call(W, "POST", f"/v1/assets/{asset['id']}/copyable", {"expected": asset["revision"], "copyable": want}, wt)
    print(f"   可复制 {len(procs) - len(secret)} 条，不可复制 {len(secret)} 条（3BJ 对接）")

    for fc in cfg["factories"]:
        if not fc["claim"]:
            continue
        fid = state["factories"][fc["key"]]["id"]
        P = f"/v1/factories/{fid}"
        step(f"{fc['name']}：组织、人员、策略")
        t = call(F, "POST", f"{P}/login", {"loginName": fc["sa"]["login"], "password": fc["sa"]["password"]})["token"]
        units: dict[str, str] = {}

        def make_units(nodes, parent=None):
            for n in nodes:
                u = call(F, "POST", f"{P}/org-units", {"name": n["name"], "parentId": parent}, t)
                units[n["name"]] = u["id"]
                make_units(n.get("children", []), u["id"])
        make_units(fc["org"])
        people = {}
        for p in fc["people"]:
            acc = call(F, "POST", f"{P}/people", {"loginName": p["login"], "displayName": p["display"]}, t)["account"]
            people[p["login"]] = acc["id"]
            call(F, "POST", f"{P}/grants", {"personId": acc["id"], "role": p["role"]}, t)
            if p.get("org"):
                call(F, "POST", f"{P}/people/{acc['id']}/org", {"orgUnitId": units[p["org"]]}, t)
            if p.get("disabled"):
                call(F, "POST", f"{P}/people/{acc['id']}/disable", {}, t)
        call(F, "PUT", f"{P}/client-policy", fc["policy"], t)
        print(f"   组织 {len(units)} 个，人员 {len(people)} 人（默认密码 登录名+123456）")
        state["factories"][fc["key"]].update({"people": people, "units": units, "sa": fc["sa"]["login"]})

        step(f"{fc['name']}：等平台工艺下发到厂")
        wait_until(lambda: sum(1 for x in call(F, "GET", f"{P}/assets", token=t) or [] if x.get("level") == "platform") >= len(procs),
                   "平台副本到厂", 120)

        if fc.get("factory_processes") or fc.get("factory_projects"):
            step(f"{fc['name']}：厂级工艺和真实工程")
            batch = legacy_batch(root, fc["factory_processes"], fc["factory_projects"], "factory")
            res = call(F, "POST", f"{P}/legacy-import", batch, t)
            check_import(res, len(fc["factory_processes"]), len(fc["factory_projects"]), "厂级")
            publish_all(F, P, t, res["processes"])
            publish_all(F, P, t, res["projects"])

        per = fc.get("personal")
        if per:
            step(f"{fc['name']}：{per['owner']} 的个人件")
            pt = call(F, "POST", f"{P}/login", {"loginName": per["owner"], "password": per["owner"] + "123456"})["token"]
            batch = legacy_batch(root, per["processes"], per["projects"], "personal")
            res = call(F, "POST", f"{P}/legacy-import", batch, pt)
            check_import(res, len(per["processes"]), len(per["projects"]), "个人")

    if a.with_history:
        history(cfg, state, F)

    with open(STATE, "w", encoding="utf-8") as f:
        json.dump(state, f, ensure_ascii=False, indent=2)
    step(f"完成，身份记在 {os.path.relpath(STATE, ROOT)}")


def history(cfg: dict, state: dict, F: str):
    """以焊工身份经示教器的焊次上报接口写过去 N 天的焊次。"""
    h = cfg["history"]
    fc = next(x for x in cfg["factories"] if x["key"] == h["factory"])
    fid = state["factories"][h["factory"]]["id"]
    P = f"/v1/factories/{fid}"
    rnd = random.Random(h["seed"])
    st = call(F, "POST", f"{P}/login", {"loginName": fc["sa"]["login"], "password": fc["sa"]["password"]})["token"]
    projects = [x for x in call(F, "GET", f"{P}/assets", token=st) or [] if x.get("kind") == "project" and x.get("level") == "factory"]
    if not projects:
        raise SystemExit("厂里没有厂级工程，写不了历史焊次")
    step(f"历史焊次：{h['days']} 天")
    today = datetime.now(timezone.utc).astimezone()
    total = 0
    for who in h["welders"]:
        tok = call(F, "POST", f"{P}/pad/login", {"loginName": who, "password": who + "123456", "appVersionName": "seed"})["token"]
        facts = []
        for d in range(h["days"], 0, -1):
            day = today - timedelta(days=d)
            lo, hi = h["runs_per_weekend"] if day.weekday() >= 5 else h["runs_per_workday"]
            for _ in range(rnd.randint(lo, hi) // len(h["welders"]) + rnd.randint(0, 1)):
                pj = rnd.choice(projects)
                mm = rnd.randint(*h["length_mm"])
                sec = max(1, round(mm / rnd.uniform(*h["speed_mm_s"])))
                at = day.replace(hour=rnd.randint(8, 17), minute=rnd.randint(0, 59), second=0, microsecond=0)
                facts.append({
                    "id": str(uuid.UUID(int=rnd.getrandbits(128))),
                    "creatorId": state["factories"][h["factory"]]["people"][who],
                    "clientId": state["devices"][rnd.choice(h["devices"])]["id"],
                    "projectId": pj["id"], "projectName": pj["name"], "weldKind": pj.get("weldKind") or "single",
                    "lengthMm": mm, "durationSec": sec, "occurredAt": at.isoformat(),
                })
        res = call(F, "POST", f"{P}/pad/weld-facts", {"facts": facts}, tok)
        rej = res.get("rejected") or []
        if rej:
            raise SystemExit(f"{who} 有焊次被拒：{rej[:3]}")
        total += len(facts)
        print(f"   {who}：{len(facts)} 条")
    print(f"   共 {total} 条")


if __name__ == "__main__":
    try:
        main()
    except ApiError as e:
        sys.exit(f"接口失败：{e}")

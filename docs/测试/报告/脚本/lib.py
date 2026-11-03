"""全系统测试用的小工具：调接口、查厂库、记结果。结果累积在 results.json，update_record.py 回填执行记录。"""
from __future__ import annotations
import json, os, subprocess, sys, time, urllib.request, urllib.error

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", ".."))
HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.path.join(HERE, "results.json")
W = "http://localhost:52080"
F = "http://localhost:52081"
STATE = json.load(open(os.path.join(ROOT, "scripts/testenv/state.local.json"), encoding="utf-8"))
FA = STATE["factories"]["A"]["id"]; FB = STATE["factories"]["B"]["id"]; FC = STATE["factories"]["C"]["id"]


def env_value(path, key):
    for line in open(os.path.join(ROOT, path), encoding="utf-8"):
        if line.startswith(key + "="):
            return line.split("=", 1)[1].strip()


WAN_PW = env_value("global/.env", "WMESH_ADMIN_PASSWORD")


def call(base, method, path, body=None, token=None, raw=False, timeout=60, headers=None):
    """返回 (状态码, 解析后的 body)；不抛 HTTP 错误。"""
    data = None if body is None else (body if isinstance(body, bytes) else json.dumps(body).encode())
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            b, st = r.read(), r.status
    except urllib.error.HTTPError as e:
        b, st = e.read(), e.code
    except Exception as e:  # 超时、拒连
        return -1, str(e)
    if raw:
        return st, b
    try:
        return st, (json.loads(b) if b else None)
    except Exception:
        return st, b.decode("utf-8", "replace")


def wan_login(pw=None):
    st, b = call(W, "POST", "/v1/login", {"loginName": "wan", "password": pw or WAN_PW})
    assert st == 200, (st, b)
    return b["token"]


def fac_login(login, pw=None, fid=None):
    fid = fid or (FB if login.endswith("b") or login.startswith("hangongb") else FA)
    st, b = call(F, "POST", f"/v1/factories/{fid}/login", {"loginName": login, "password": pw or default_pw(login)})
    assert st == 200, (login, st, b)
    return b["token"]


def default_pw(login):
    return {"changzhang": "changzhang123456", "changzhangb": "changzhangb123456"}.get(login, login + "123456")


def pad_login(login, pw=None, fid=None, **kw):
    fid = fid or FA
    body = {"loginName": login, "password": pw or default_pw(login), "appVersion": 82, "appVersionName": "test"}
    body.update(kw)
    st, b = call(F, "POST", f"/v1/factories/{fid}/pad/login", body)
    assert st == 200, (st, b)
    return b["token"]


def psql(sql, db=None, fid=None, svc="factory"):
    if db is None:
        db = "wmesh_fac_" + (fid or FA).replace("-", "")
    out = subprocess.run(["docker", "compose", "exec", "-T", "db", "sh", "-c", f'psql -U "$POSTGRES_USER" -d {db} -Atc "$0"', sql],
                         cwd=os.path.join(ROOT, svc), capture_output=True, text=True)
    return (out.stdout + out.stderr).strip()


def short(x, n=300):
    s = x if isinstance(x, str) else json.dumps(x, ensure_ascii=False)
    return s if len(s) <= n else s[:n] + "…"


def record(case, result, evidence, batch=None):
    """result: 通过/失败/阻塞/需真机/需人工/未执行/部分通过"""
    data = json.load(open(RESULTS, encoding="utf-8")) if os.path.exists(RESULTS) else {}
    data[case] = {"result": result, "evidence": evidence, "batch": batch, "at": time.strftime("%m-%d %H:%M")}
    json.dump(data, open(RESULTS, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(f"[{case}] {result}：{evidence}")


_TOK = os.path.join(HERE, ".tokens.json")


def tok(who):
    """缓存令牌，避免 WAN 登录限速（每来源 10 次、12 秒补 1 次）。who='wan' 或厂端登录名。"""
    cache = json.load(open(_TOK)) if os.path.exists(_TOK) else {}
    t = cache.get(who)
    if t:
        if who == "wan":
            ok = call(W, "GET", "/v1/me", token=t)[0] == 200
        else:
            fid = FB if who in ("changzhangb", "hangongb1") else FA
            ok = call(F, "GET", f"/v1/factories/{fid}/me", token=t)[0] == 200
        if ok:
            return t
    t = wan_login() if who == "wan" else fac_login(who)
    cache[who] = t
    json.dump(cache, open(_TOK, "w"))
    return t

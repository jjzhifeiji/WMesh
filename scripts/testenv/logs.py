#!/usr/bin/env python3
"""查本机测试 Loki 里的三端日志（WAN、厂端容器日志，示教器经厂端 :3500 推来的日志）。

    scripts/testenv/logs.py                                  # 最近 15 分钟全部日志
    scripts/testenv/logs.py -s pad -l warn,error --since 2h  # 示教器 2 小时内的告警和错误
    scripts/testenv/logs.py -s factory -k /pad/assets -k 400 # 厂端含两个关键字的行（同时含）
    scripts/testenv/logs.py --client <设备id> --from "11-04 19:48" --to "11-04 20:20" -o out.log
    scripts/testenv/logs.py check                            # 确认两端 alloy 只推本机 Loki、日志到了
    scripts/testenv/logs.py labels                           # 列服务、级别现有取值

level 标签统一大写（INFO / WARN / ERROR），-l 不分大小写。厂、设备按结构化元数据 factory、client 筛。
"""
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

LOKI = f"http://127.0.0.1:{os.environ.get('TESTLOG_LOKI_PORT', '3100')}"
SHARED_HOST = "154.82.81.16"
SERVICES = {
    "wan": "wmesh-global", "global": "wmesh-global",
    "factory": "wmesh-factory", "厂端": "wmesh-factory",
    "pad": "wmesh-pad", "示教器": "wmesh-pad",
}
LEVELS = ("INFO", "WARN", "ERROR")
PAGE = 5000
# 容器访问宿主的地址都算本机；其它地址一律不认。
LOCAL_HOSTS = ("host.docker.internal", "127.0.0.1", "localhost")


def service_names(raw: str | None) -> list[str]:
    """-s 取值换成 Loki service 标签值；all 或空表示不限。"""
    if not raw or raw == "all":
        return []
    out = []
    for part in raw.split(","):
        part = part.strip()
        if part:
            out.append(SERVICES.get(part.lower(), SERVICES.get(part, part)))
    return out


def level_names(raw: str | None) -> list[str]:
    """-l 不分大小写，统一成大写标签值；只认 INFO / WARN / ERROR。"""
    if not raw:
        return []
    out = []
    for part in raw.split(","):
        lv = part.strip().upper()
        if lv == "WARNING":
            lv = "WARN"
        if lv not in LEVELS:
            raise ValueError(f"不认识的级别：{part}（只有 info、warn、error）")
        out.append(lv)
    return out


def logql_str(v: str) -> str:
    """LogQL 字符串字面量：反斜杠和双引号转义。"""
    return '"' + v.replace("\\", "\\\\").replace('"', '\\"') + '"'


def build_query(services: list[str], levels: list[str], factory: str | None = None,
                client: str | None = None, keywords: list[str] | None = None,
                excludes: list[str] | None = None, regex: str | None = None) -> str:
    """拼 LogQL：服务、级别走索引标签，厂、设备走结构化元数据，关键字逐个过滤（同时含）。"""
    sel = ['product="wmesh"']
    if len(services) == 1:
        sel.append(f"service={logql_str(services[0])}")
    elif services:
        sel.append("service=~" + logql_str("|".join(services)))
    if len(levels) == 1:
        sel.append(f"level={logql_str(levels[0])}")
    elif levels:
        sel.append("level=~" + logql_str("|".join(levels)))
    q = "{" + ", ".join(sel) + "}"
    for kw in keywords or []:
        q += f" |= {logql_str(kw)}"
    for kw in excludes or []:
        q += f" != {logql_str(kw)}"
    if regex:
        q += f" |~ {logql_str(regex)}"
    if factory:
        q += f" | factory={logql_str(factory)}"
    if client:
        q += f" | client={logql_str(client)}"
    return q


DURATION = re.compile(r"^(\d+)([smhd])$")


def parse_since(raw: str) -> dt.timedelta:
    """30m、2h、1d 这类时长。"""
    m = DURATION.match(raw.strip())
    if not m:
        raise ValueError(f"时长写成 30s / 15m / 2h / 1d：{raw}")
    n, unit = int(m.group(1)), m.group(2)
    return dt.timedelta(**{{"s": "seconds", "m": "minutes", "h": "hours", "d": "days"}[unit]: n})


def parse_time(raw: str, now: dt.datetime | None = None) -> dt.datetime:
    """本地时间：「19:48」当天、「11-04 19:48」今年、「2026-11-04 19:48[:05]」、ISO 带时区、纯数字按 epoch 秒。"""
    now = now or dt.datetime.now().astimezone()
    s = raw.strip()
    if re.fullmatch(r"\d{10}(\.\d+)?", s):
        return dt.datetime.fromtimestamp(float(s)).astimezone()
    try:
        t = dt.datetime.fromisoformat(s)
        return t.astimezone() if t.tzinfo else t.replace(tzinfo=now.tzinfo)
    except ValueError:
        pass
    for fmt, fill in (("%H:%M:%S", "day"), ("%H:%M", "day"), ("%m-%d %H:%M:%S", "year"), ("%m-%d %H:%M", "year")):
        try:
            t = dt.datetime.strptime(s, fmt)
        except ValueError:
            continue
        if fill == "day":
            t = t.replace(year=now.year, month=now.month, day=now.day)
        else:
            t = t.replace(year=now.year)
        return t.replace(tzinfo=now.tzinfo)
    raise ValueError(f"认不出的时间：{raw}")


def time_range(args, now: dt.datetime | None = None) -> tuple[dt.datetime, dt.datetime]:
    """--from/--to 优先，否则 --since 往回推。"""
    now = now or dt.datetime.now().astimezone()
    end = parse_time(args.to, now) if args.to else now
    start = parse_time(args.frm, now) if args.frm else end - parse_since(args.since)
    if start >= end:
        raise ValueError("开始时间要早于结束时间")
    return start, end


def ns(t: dt.datetime) -> int:
    return int(t.timestamp()) * 1_000_000_000 + t.microsecond * 1000


def loki_get(base: str, path: str, params: dict) -> dict:
    url = f"{base}{path}?{urllib.parse.urlencode(params)}"
    with urllib.request.urlopen(url, timeout=30) as r:
        return json.load(r)


def fetch(base: str, query: str, start: int, end: int, limit: int):
    """正序分页拉，同一纳秒边界上的重复行去掉；limit<=0 不限条数。"""
    seen_edge: set[tuple[str, str]] = set()
    got = 0
    while True:
        page = PAGE if limit <= 0 else min(PAGE, limit - got)
        if page <= 0:
            return
        data = loki_get(base, "/loki/api/v1/query_range", {
            "query": query, "start": start, "end": end, "limit": page, "direction": "forward",
        })
        rows = []
        for st in data.get("data", {}).get("result", []):
            labels = st.get("stream", {})
            for ts, line in st.get("values", []):
                rows.append((int(ts), line, labels))
        rows.sort(key=lambda r: r[0])
        fresh = [r for r in rows if (str(r[0]), r[1]) not in seen_edge]
        # 一整页都是上页边界上见过的行（同一纳秒挤满一页），再翻还是这页，停下免得死循环。
        if rows and not fresh:
            return
        for r in fresh:
            yield r
            got += 1
            if 0 < limit <= got:
                return
        if len(rows) < page:
            return
        last = rows[-1][0]
        seen_edge = {(str(r[0]), r[1]) for r in rows if r[0] == last}
        start = last


def render(ts: int, line: str, labels: dict, fmt: str) -> str:
    t = dt.datetime.fromtimestamp(ts / 1e9).astimezone()
    if fmt == "jsonl":
        return json.dumps({
            "time": t.isoformat(timespec="milliseconds"),
            "service": labels.get("service", ""),
            "level": labels.get("level", ""),
            "factory": labels.get("factory", ""),
            "client": labels.get("client", ""),
            "line": line,
        }, ensure_ascii=False)
    return f"{t:%m-%d %H:%M:%S.%f}"[:-3] + f" {labels.get('service', '-'):<13} {labels.get('level', '-'):<5} {line}"


def cmd_query(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(prog="logs.py", description="查本机测试 Loki 的三端日志")
    ap.add_argument("-s", "--service", help="wan / factory / pad，逗号分隔多个，默认全部")
    ap.add_argument("-l", "--level", help="info / warn / error，逗号分隔多个，不分大小写")
    ap.add_argument("--factory", help="工厂 id（结构化元数据 factory）")
    ap.add_argument("--client", help="示教器设备 id（结构化元数据 client）")
    ap.add_argument("-k", "--keyword", action="append", default=[], help="行里要含的字，可写多个（同时含）")
    ap.add_argument("-v", "--exclude", action="append", default=[], help="行里不能含的字，可写多个")
    ap.add_argument("-x", "--regex", help="行要匹配的正则（RE2）")
    ap.add_argument("--since", default="15m", help="往回查多久，默认 15m；有 --from 时不用")
    ap.add_argument("--from", dest="frm", help="开始时间（本地）：19:48、11-04 19:48、2026-11-04 19:48:05")
    ap.add_argument("--to", help="结束时间（本地），默认现在")
    ap.add_argument("-n", "--limit", type=int, default=2000, help="最多多少行，0 不限，默认 2000")
    ap.add_argument("--format", choices=("text", "jsonl"), default="text")
    ap.add_argument("-o", "--out", help="写到文件，不打屏")
    ap.add_argument("--loki", default=LOKI, help=f"Loki 地址，默认 {LOKI}")
    ap.add_argument("--print-query", action="store_true", help="只打印拼好的 LogQL")
    a = ap.parse_args(argv)
    try:
        q = build_query(service_names(a.service), level_names(a.level), a.factory, a.client,
                        a.keyword, a.exclude, a.regex)
        start, end = time_range(a)
    except ValueError as e:
        ap.error(str(e))
    if a.print_query:
        print(q)
        return 0
    out = open(a.out, "w", encoding="utf-8") if a.out else sys.stdout
    n = 0
    try:
        for ts, line, labels in fetch(a.loki, q, ns(start), ns(end), a.limit):
            print(render(ts, line, labels, a.format), file=out)
            n += 1
    except urllib.error.URLError as e:
        print(f"连不上本机 Loki {a.loki}：{e}（先跑 scripts/testenv/reset.sh 或手动起 scripts/testenv/loki）", file=sys.stderr)
        return 1
    finally:
        if a.out:
            out.close()
    print(f"{n} 行 · {start:%m-%d %H:%M:%S} ～ {end:%m-%d %H:%M:%S} · {q}" + (f" → {a.out}" if a.out else ""),
          file=sys.stderr)
    return 0


def cmd_labels(argv: list[str]) -> int:
    base = argv[0] if argv else LOKI
    for name in ("service", "level"):
        vals = loki_get(base, f"/loki/api/v1/label/{name}/values", {"since": "720h"}).get("data", [])
        print(f"{name}: {', '.join(vals) or '（无）'}")
    return 0


def docker(*args: str) -> str:
    return subprocess.run(["docker", *args], capture_output=True, text=True, check=False).stdout


def alloy_container(project: str) -> str:
    return docker("ps", "-q", "--filter", f"label=com.docker.compose.project={project}",
                  "--filter", "label=com.docker.compose.service=alloy").strip()


def loki_url_ok(url: str) -> bool:
    """LOKI_URL 只许指向本机 Loki。"""
    host = urllib.parse.urlparse(url).hostname or ""
    return SHARED_HOST not in url and host in LOCAL_HOSTS


WRITE_HOST = re.compile(r'^loki_write_\w+\{[^}]*host="([^"]+)"', re.M)


def write_hosts(metrics: str) -> set[str]:
    """从 alloy /metrics 里取 loki.write 实际发往的主机。"""
    return {m.group(1) for m in WRITE_HOST.finditer(metrics)}


def cmd_check(argv: list[str]) -> int:
    base = argv[0] if argv else LOKI
    bad = []
    for project in ("wmesh-global", "wmesh-factory"):
        cid = alloy_container(project)
        if not cid:
            bad.append(f"{project} 的 alloy 没在跑")
            continue
        env = json.loads(docker("inspect", "--format", "{{json .Config.Env}}", cid) or "[]")
        url = next((e.split("=", 1)[1] for e in env if e.startswith("LOKI_URL=")), "")
        print(f"{project} alloy LOKI_URL={url}")
        if not loki_url_ok(url):
            bad.append(f"{project} alloy 的 LOKI_URL 不是本机 Loki：{url}")
        logs = subprocess.run(["docker", "logs", cid], capture_output=True, text=True, check=False)
        if SHARED_HOST in logs.stdout + logs.stderr:
            bad.append(f"{project} alloy 日志里出现 {SHARED_HOST}")
        # alloy 镜像没有 curl，用 bash /dev/tcp 读自己的 /metrics，看 loki.write 实际发往哪。
        metrics = docker("exec", cid, "bash", "-c",
                         "exec 3<>/dev/tcp/127.0.0.1/12345; printf 'GET /metrics HTTP/1.0\\r\\n\\r\\n' >&3; cat <&3")
        hosts = write_hosts(metrics)
        print(f"{project} alloy 实际发往：{', '.join(sorted(hosts)) or '（还没发）'}")
        for h in hosts:
            if (h.rsplit(":", 1)[0]) not in LOCAL_HOSTS:
                bad.append(f"{project} alloy 往 {h} 发过日志")
    end = dt.datetime.now().astimezone()
    start = end - dt.timedelta(minutes=30)
    for svc in ("wmesh-global", "wmesh-factory"):
        n = 0
        # alloy 采到推出有几秒延迟，最多等 60 秒。
        for _ in range(30):
            try:
                n = sum(1 for _ in fetch(base, build_query([svc], []), ns(start), ns(dt.datetime.now().astimezone()), 1))
            except urllib.error.URLError as e:
                bad.append(f"连不上本机 Loki {base}：{e}")
                break
            if n:
                break
            time.sleep(2)
        print(f"本机 Loki {svc}：{'有日志' if n else '30 分钟内没有日志'}")
        if not n:
            bad.append(f"本机 Loki 查不到 {svc} 的日志")
    try:
        # 只看本次检查的时间窗：Loki 留 30 天，修复前推进来的小写旧数据不该让每次检查都不通过。
        levels = loki_get(base, "/loki/api/v1/label/level/values",
                          {"start": ns(start), "end": ns(dt.datetime.now().astimezone())}).get("data", [])
        lower = [v for v in levels if v != v.upper()]
        if lower:
            bad.append(f"level 标签有小写取值 {lower}（alloy 没统一大小写）")
    except urllib.error.URLError:
        pass
    if bad:
        print("日志检查不通过：\n  " + "\n  ".join(bad), file=sys.stderr)
        return 1
    print("日志检查通过：两端 alloy 只推本机 Loki，WAN、厂端日志都已落本机")
    return 0


def main(argv: list[str]) -> int:
    if argv and argv[0] == "check":
        return cmd_check(argv[1:])
    if argv and argv[0] == "labels":
        return cmd_labels(argv[1:])
    return cmd_query(argv)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

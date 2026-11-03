from lib import *
import urllib.parse as u
from collections import defaultdict
B = "1"
t = tok("wan"); ta = tok("changzhang")
q = u.urlencode({"from": "2026-09-01T00:00:00Z", "to": "2026-12-01T00:00:00Z"})
wr = call(W, "GET", "/v1/weld-reports?" + q + "&factoryId=" + FA, token=t)[1]
wall = call(W, "GET", "/v1/weld-reports?" + q, token=t)[1]
agg = defaultdict(lambda: [0, 0, 0])
for r in wr:
    a = agg[r["projectName"]]; a[0] += r["runCount"]; a[1] += r["lengthMm"]; a[2] += r["durationSec"]
fr = call(F, "GET", f"/v1/factories/{FA}/weld-reports?" + q + "&group=project", token=ta)[1]
fagg = {r["projectName"]: [r["runCount"], r["lengthMm"], r["durationSec"]] for r in fr}
db = psql("select count(*), sum(length_mm), sum(duration_sec) from weld_facts")
tot_w = [sum(a[i] for a in agg.values()) for i in range(3)]
keys = sorted(set(k for r in wall for k in r))
same = dict(agg) == fagg
avg = round(tot_w[1] / tot_w[0], 1)
record("W-60", "通过" if same and "person" not in json.dumps(keys) and "org" not in json.dumps(keys).lower() else "失败",
       f"WAN 按厂按日按工程汇总 {len(wr)} 行，按工程合计与厂端 group=project 完全一致={same}；合计 次数/焊长/时长 WAN={tot_w}，厂库 weld_facts={db}；次均焊长 {avg}mm；WAN 字段 {keys}，无人员/组织；不选厂时 {len(wall)} 行（B 厂无焊次）", B)

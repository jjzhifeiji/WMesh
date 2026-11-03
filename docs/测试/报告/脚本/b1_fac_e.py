from lib import *
import time, urllib.parse as u
B = "1"
P = f"/v1/factories/{FA}"
ts, ta, to = tok("changzhang"), tok("gongyi1"), tok("hangong1")
C = lambda p, b=None, m="POST", t=None: call(F, m, P + p, b, token=t or ts)
al = C("/assets", m="GET")[1]
per = [a for a in al if a["level"] == "personal" and a["name"] == "1K6.5mmD"][0]
nc = [a for a in al if a["level"] == "platform" and not a["copyable"]][0]
r_adm = C(f"/assets/{per['id']}/promote", {"folderId": ""}, t=ta)
r_op = C(f"/assets/{per['id']}/promote", {"folderId": ""}, t=to)
r_sa = C(f"/assets/{per['id']}/promote", {"folderId": ""})
r_sa2 = C(f"/assets/{per['id']}/promote", {"folderId": ""})
r_nc = C(f"/assets/{nc['id']}/promote", {"folderId": ""})
src_after = C(f"/assets/{per['id']}", m="GET")[1]
g = lambda r: (r[0], r[1].get("code"), r[1].get("status"), r[1].get("level")) if isinstance(r[1], dict) and "code" in r[1] else (r[0], short(r[1], 40))
record("F-34", "通过" if r_sa[0] in (200, 201) and r_op[0] == 403 and r_nc[0] >= 400 and src_after["level"] == "personal" else "失败",
       f"⚠口径K4。管理员 gongyi1 升他人个人件→{g(r_adm)}（K2 下管理员看不到他人个人件）；操作员→{g(r_op)}；超管→{g(r_sa)}；再升一次→{g(r_sa2)}；升平台不可复制副本→{g(r_nc)}；源件仍是个人级 修订 {src_after['revision']}", B)
# F-35
st = C(f"/assets/{nc['id']}/content", m="GET")
sn = C(f"/assets/{nc['id']}/snapshot", m="GET")
meta = C(f"/assets/{nc['id']}", m="GET")
fe = open(os.path.join(ROOT, "factory/frontend/src")).name if False else None
import subprocess
hit = subprocess.run(["grep", "-rn", "不可复制，不显示工艺参数", os.path.join(ROOT, "factory/frontend/src")], capture_output=True, text=True).stdout.strip().splitlines()
record("F-35", "通过" if st[0] == 403 and meta[0] == 200 and hit else "失败",
       f"平台不可复制「{nc['name'][-12:]}」：元数据→{meta[0]}；content→{st[0]} {st[1]}；snapshot→{sn[0]}；前端提示文案在 {hit[0].split(':')[0].split('frontend/')[-1] if hit else '未找到'}（界面在 F-19 浏览器核对）", B)
# F-37 旧文件导入
DD = os.path.join(ROOT, "docs/device-date")
c1 = json.loads(open(os.path.join(DD, "processes/默认工艺.json"), encoding="utf-8").read()); c1["name"] = "F37导入"
batch = {"processes": [{"path": "F37/F37导入.json", "content": json.dumps(c1, ensure_ascii=False)}], "projects": []}
i1 = C("/legacy-import", batch, t=to)
c1["name"] = "F37导入厂级"; batch2 = {"processes": [{"path": "F37/F37导入厂级.json", "content": json.dumps(c1, ensure_ascii=False)}], "projects": [], "level": "factory"}
i2 = C("/legacy-import", batch2, t=ta)
i3 = C("/legacy-import", dict(batch2, processes=[{"path": "F37/x.json", "content": json.dumps(dict(c1, name="F37操作员厂级"), ensure_ascii=False)}]), t=to)
c1["name"] = "F37管理员默认"
i4 = C("/legacy-import", {"processes": [{"path": "F37/y.json", "content": json.dumps(c1, ensure_ascii=False)}], "projects": []}, t=ta)
lv = lambda r: [x["level"] for x in (r[1].get("processes") or [])] if isinstance(r[1], dict) else r
record("F-37", "通过" if lv(i1) == ["personal"] and lv(i2) == ["factory"] and i3[0] == 403 and lv(i4) == ["personal"] else "失败",
       f"⚠口径K7。操作员默认→{i1[0]} {lv(i1)}；管理员选厂级→{i2[0]} {lv(i2)}；操作员选厂级→{i3[0]} {short(i3[1],40)}；管理员不选→{lv(i4)}（默认个人）", B)
# F-38 补拉（在线时）
t0 = time.time(); s1 = C("/assets/sync", {}); d1 = round(time.time() - t0, 3)
t0 = time.time(); s2 = C("/templates/sync", {}); d2 = round(time.time() - t0, 3)
record("F-38", "部分通过", f"WAN 在线：assets/sync→{s1[0]} 用时 {d1}s，templates/sync→{s2[0]} 用时 {d2}s，立即返回；WAN 断开时的表现放批 6（停 WAN）补测", B)
# F-40 报表
q = u.urlencode({"from": "2026-09-01T00:00:00Z", "to": "2026-12-01T00:00:00Z", "tz": "Asia/Shanghai"})
res = {}
for g_ in ["person", "project", "org", "day"]:
    s, r = C(f"/weld-reports?{q}&group={g_}", m="GET")
    res[g_] = (s, len(r) if isinstance(r, list) else r, sum(x.get("runCount", 0) for x in r) if isinstance(r, list) else None, sum(x.get("lengthMm", 0) for x in r) if isinstance(r, list) else None)
db = psql("select count(*), sum(length_mm) from weld_facts")
op = C(f"/weld-reports?{q}&group=person", m="GET", t=to)
dbh1 = psql("select count(*), sum(length_mm) from weld_facts f join people p on p.id=f.creator_id where p.login_name='hangong1'")
runs = C(f"/weld-runs?{u.urlencode({'from':'2026-09-01T00:00:00Z','to':'2026-12-01T00:00:00Z'})}&limit=5", m="GET")
runs_op = C(f"/weld-runs?{u.urlencode({'from':'2026-09-01T00:00:00Z','to':'2026-12-01T00:00:00Z'})}&limit=500", m="GET", t=to)
others = [x for x in (runs_op[1].get("rows", runs_op[1]) if isinstance(runs_op[1], (dict, list)) else []) if isinstance(x, dict) and x.get("creatorLogin", x.get("loginName", "hangong1")) != "hangong1"] if runs_op[0] == 200 else "?"
record("F-40", "通过" if all(v[0] == 200 for v in res.values()) and f"{res['person'][2]}|{res['person'][3]}" == db and op[0] == 200 else "失败",
       f"各维度（状态,行数,次数和,焊长和）{res}；库 weld_facts {db}；操作员 hangong1 按人→{op[0]} {short(op[1],120)}（库里本人 {dbh1}）；下钻 weld-runs→{runs[0]} {short(runs[1],150)}；操作员下钻里非本人行={others if isinstance(others,str) else len(others)}", B)

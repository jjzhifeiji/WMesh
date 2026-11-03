from lib import *
import time
B = "1"
P = f"/v1/factories/{FA}"
ts = tok("changzhang")
C = lambda p, b=None, m="POST", t=None: call(F, m, P + p, b, token=t or ts)
cl = C("/clients", m="GET")[1]
db = psql("select client_id, count(*), sum(length_mm) from weld_facts group by client_id order by 1")
last = psql("select f.client_id, p.login_name from weld_facts f join people p on p.id=f.creator_id where (f.client_id, f.occurred_at) in (select client_id, max(occurred_at) from weld_facts group by client_id)")
mine = {c["id"]: (c["weldRunCount"], c["weldLengthMm"], c["lastUserLogin"]) for c in cl}
dbm = {l.split("|")[0]: (int(l.split("|")[1]), int(l.split("|")[2])) for l in db.splitlines() if l}
lastm = {l.split("|")[0]: l.split("|")[1] for l in last.splitlines() if l}
match = all(mine[k][:2] == v and mine[k][2] == lastm.get(k) for k, v in dbm.items())
cid = cl[0]["id"]
n1 = C(f"/clients/{cid}", {"name": "焊房东"}, m="PATCH")
nn = [c["nickname"] for c in C("/clients", m="GET")[1] if c["id"] == cid]
n2 = C(f"/clients/{cid}", {"name": ""}, m="PATCH")
nn2 = [c["nickname"] for c in C("/clients", m="GET")[1] if c["id"] == cid]
record("F-20", "通过" if match and nn == ["焊房东"] and nn2 == [""] else "失败",
       f"{len(cl)} 台：焊次数/累计焊长/最近使用人与 weld_facts 聚合一致={match}（{ {mine[k][2]: mine[k][:2] for k in mine} }）；设昵称→{n1[0]} 显示 {nn}；留空→{n2[0]} 显示 {nn2}；设备名、识别号随 WAN（W-22 改名已同步）", B)
# F-22 / 部分 F-21（接口侧）
pol = C("/client-policy", m="GET")[1]
body = {"maxCachedProjects": pol["maxCachedProjects"], "cacheScope": pol["cacheScope"], "persistUnwrapKey": pol["persistUnwrapKey"], "keyTtlSeconds": 3600, "encryptPouch": pol["encryptPouch"]}
ta = tok("gongyi1"); to = tok("hangong3")
r_adm = C("/client-policy", body, m="PUT", t=ta)
r_op = C("/client-policy", body, m="PUT", t=to)
record("F-22", "通过" if r_adm[0] == 403 and r_op[0] == 403 else "失败", f"管理员 gongyi1 PUT client-policy→{r_adm[0]}；操作员→{r_op[0]}", B)
# F-43 首页
h = call(F, "GET", "/healthz")[1]
cat = C("/catalog", m="GET")[1]
record("F-43", "通过" if h["db"] == "ok" and h["oss"] == "ok" else "失败",
       f"healthz：db={h['db']} oss={h['oss']} version={h['versionName']}；名册 人员 {len(cat['people'])}、组织 {len(cat['orgUnits'])}、角色授权 {len([g for g in cat['roleGrants'] if g['status']=='active'])}（首页计数取这三项；界面显示待 F-19 浏览器核对）", B)
# F-41 演示焊次
d1 = C("/weld-reports/demo", {})
record("F-41", "通过" if d1[0] in (403, 404) and h.get("demoData") is False else "失败", f"healthz demoData={h.get('demoData')}；未设 WMESH_DEMO_DATA 时调 demo→{d1[0]} {short(d1[1],50)}；有开关时的按钮未测（需改环境变量重启，价值低）", B)

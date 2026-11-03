from lib import *
import re
import time, copy
B = "1"
t = tok("wan")
A = lambda p, b=None, m="POST": call(W, m, p, b, token=t)
plat = A("/v1/assets", m="GET")[1]
pub = [x for x in plat if x["copyable"] and x["status"] == "available"]
src = A(f"/v1/assets/{pub[0]['id']}/content", m="GET")[1]["content"]
proc_body = json.loads(src); proc_body["name"] = "W30测试工艺"
# W-30
s1, p1 = A("/v1/assets", {"kind": "process", "name": "W30测试工艺", "content": json.dumps(proc_body, ensure_ascii=False), "weldKind": "single", "copyable": True})
proj = json.load(open(".proj_single.json"))
proj[0]["processId"] = pub[0]["id"]; proj[0]["extraProcesses"] = []
s2, j1 = A("/v1/assets", {"kind": "project", "name": "W30测试工程", "content": json.dumps(proj, ensure_ascii=False), "weldKind": "single"})
codes_ok = re.match(r"GY-W-\d{6}$", p1.get("code", "")) and re.match(r"GC-W-\d{6}$", j1.get("code", "")) if s1 == 201 and s2 == 201 else False
record("W-30", "通过" if s1 == 201 and s2 == 201 and p1["status"] == "draft" and j1["status"] == "draft" and codes_ok else "失败",
       f"工艺→{s1} {p1.get('code')} {p1.get('status')}；工程→{s2} {j1.get('code') if isinstance(j1,dict) else j1} {j1.get('status') if isinstance(j1,dict) else ''}；deps 自动补={[d['id'][:8] for d in (j1.get('deps') or [])] if isinstance(j1,dict) else ''}；接口无改编号字段（只读）", B)
# W-31
proc_body["current"] = 201
s, r = A(f"/v1/assets/{p1['id']}/content", {"expected": p1["revision"], "content": json.dumps(proc_body, ensure_ascii=False)})
p1b = A(f"/v1/assets/{p1['id']}", m="GET")[1]
record("W-31", "通过" if s == 200 and p1b["revision"] == p1["revision"] + 1 and p1b["digest"] != p1["digest"] else "失败",
       f"改正文→{s}；修订 {p1['revision']}→{p1b['revision']}；摘要 {p1['digest'][:8]}→{p1b['digest'][:8]}。表单/JSON/焊道编辑器三种界面最终都走同一接口（界面未逐一点）", B)
# W-32
proc_body["current"] = 202
s, r = A(f"/v1/assets/{p1['id']}/content", {"expected": p1["revision"], "content": json.dumps(proc_body, ensure_ascii=False)})
cur = json.loads(A(f"/v1/assets/{p1['id']}/content", m="GET")[1]["content"])["current"]
record("W-32", "通过" if s == 409 and cur == 201 else "失败", f"用旧修订 {p1['revision']} 再存→{s} {short(r,80)}；原件 current 仍为 {cur}", B)
# W-37 依赖指向草稿 / 停用
s37a, r37a = A(f"/v1/assets/{j1['id']}/deps", {"expected": j1["revision"], "deps": [{"id": p1["id"], "revision": p1b["revision"], "digest": p1b["digest"]}]})
proj2 = copy.deepcopy(proj); proj2[0]["processId"] = p1["id"]
s37b, r37b = A("/v1/assets", {"kind": "project", "name": "W37引草稿", "content": json.dumps(proj2, ensure_ascii=False), "weldKind": "single"})
# W-33 发布 / 停用 / 启用 / 删除
cur = A(f"/v1/assets/{p1['id']}", m="GET")[1]
sp, rp = A(f"/v1/assets/{p1['id']}/publish", {"expected": cur["revision"]})
cur = A(f"/v1/assets/{p1['id']}", m="GET")[1]
# 工程改依赖到它，发布工程
proj3 = copy.deepcopy(proj); proj3[0]["processId"] = p1["id"]
jcur = A(f"/v1/assets/{j1['id']}", m="GET")[1]
sc, rc = A(f"/v1/assets/{j1['id']}/content", {"expected": jcur["revision"], "content": json.dumps(proj3, ensure_ascii=False)})
jcur = A(f"/v1/assets/{j1['id']}", m="GET")[1]
sjp, _ = A(f"/v1/assets/{j1['id']}/publish", {"expected": jcur["revision"]})
jcur = A(f"/v1/assets/{j1['id']}", m="GET")[1]
sd1, rd1 = A(f"/v1/assets/{p1['id']}/delete", {"expected": cur["revision"]})
sdis, _ = A(f"/v1/assets/{p1['id']}/disable", {"expected": cur["revision"]})
cur2 = A(f"/v1/assets/{p1['id']}", m="GET")[1]
# W-37b 停用后被新依赖
proj4 = copy.deepcopy(proj); proj4[0]["processId"] = p1["id"]
s37c, r37c = A("/v1/assets", {"kind": "project", "name": "W37引停用", "content": json.dumps(proj4, ensure_ascii=False), "weldKind": "single"})
# W-35 停用的改作业类型；被钉住的改
s35a, r35a = A(f"/v1/assets/{p1['id']}/weld-kind", {"expected": cur2["revision"], "weldKind": "multilayer"})
sen, _ = A(f"/v1/assets/{p1['id']}/enable", {"expected": cur2["revision"]})
cur3 = A(f"/v1/assets/{p1['id']}", m="GET")[1]
s35b, r35b = A(f"/v1/assets/{p1['id']}/weld-kind", {"expected": cur3["revision"], "weldKind": "multilayer"})
record("W-35", "通过" if s35a >= 400 and s35b >= 400 else "失败", f"停用时改作业类型→{s35a} {short(r35a,60)}；启用后（被已发布工程钉住）改→{s35b} {short(r35b,60)}", B)
record("W-37", "通过" if s37a >= 400 and s37b >= 400 and s37c >= 400 else "失败",
       f"工程依赖改指草稿工艺→{s37a} {short(r37a,60)}；新建工程正文引草稿→{s37b} {short(r37b,60)}；引停用工艺→{s37c} {short(r37c,60)}", B)
# 删除未被依赖的：新建一条发布后删除，看厂端 retracted
s, p9 = A("/v1/assets", {"kind": "process", "name": "W33待删工艺", "content": json.dumps(dict(proc_body, name="W33待删工艺"), ensure_ascii=False), "weldKind": "single", "copyable": True})
A(f"/v1/assets/{p9['id']}/publish", {"expected": p9["revision"]})
ta = tok("changzhang")
seen = None
for _ in range(20):
    fa = call(F, "GET", f"/v1/factories/{FA}/assets", token=ta)[1]
    seen = [x for x in fa if x.get("sourceId") == p9["id"] or x["name"] == "W33待删工艺"]
    if seen: break
    time.sleep(1)
p9c = A(f"/v1/assets/{p9['id']}", m="GET")[1]
sdel, rdel = A(f"/v1/assets/{p9['id']}/delete", {"expected": p9c["revision"]})
after = None
for _ in range(20):
    fa = call(F, "GET", f"/v1/factories/{FA}/assets", token=ta)[1]
    after = [x for x in fa if x["id"] == (seen[0]["id"] if seen else "")]
    if after and after[0]["status"] not in ("available",): break
    time.sleep(1)
record("W-33", "通过" if sp == 200 and sd1 >= 400 and sdis == 200 and sen == 200 and sdel in (200, 204) and after and after[0]["status"] != "available" else "失败",
       f"发布→{sp}；被已发布工程依赖时删除→{sd1} {short(rd1,60)}；停用→{sdis}；启用→{sen}；另建一条发布后到 A 厂={bool(seen)}，删除→{sdel}，A 厂副本状态={after[0]['status'] if after else '已消失'} {short(after[0] if after else '',150)}", B)
# W-36 另存草稿
s36, r36 = A(f"/v1/assets/{p1['id']}/copy", {"name": "W36另存"})
record("W-36", "通过" if s36 in (200, 201) and r36["id"] != p1["id"] and r36["code"] != p1["code"] and r36["status"] == "draft" else "失败",
       f"copy→{s36}；新 id {str(r36.get('id'))[:8]} ≠ 原 {p1['id'][:8]}；编号 {r36.get('code')} ≠ {p1['code']}；状态 {r36.get('status')}", B)
json.dump({"p1": p1["id"], "j1": j1["id"]}, open(".b1_wanasset.json", "w"))

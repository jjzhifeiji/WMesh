from lib import *
import time, copy
B = "1"
t = tok("wan"); A = lambda p, b=None, m="POST": call(W, m, p, b, token=t)
j = "01a19c3e-6e95-7b89-add1-5f4eb41596cf"; pid = open(".w33p").read()
G = lambda i: A(f"/v1/assets/{i}", m="GET")[1]
proj = json.loads(A(f"/v1/assets/{j}/content", m="GET")[1]["content"])
pbody = json.loads(A(f"/v1/assets/{pid}/content", m="GET")[1]["content"])
# 草稿工艺
s, dr = A("/v1/assets", {"kind": "process", "name": "W37草稿", "content": json.dumps(dict(pbody, name="W37草稿"), ensure_ascii=False), "weldKind": "single", "copyable": True})
jc = G(j)
s37a, r37a = A(f"/v1/assets/{j}/deps", {"expected": jc["revision"], "deps": jc["deps"] + [{"id": dr["id"], "revision": dr["revision"], "digest": dr["digest"]}]})
p2 = copy.deepcopy(proj); p2[0]["processId"] = dr["id"]
s37b, r37b = A("/v1/assets", {"kind": "project", "name": "W37引草稿", "content": json.dumps(p2, ensure_ascii=False), "weldKind": "single"})
# 删被依赖
p = G(pid)
sd1, rd1 = A(f"/v1/assets/{pid}/delete", {"expected": p["revision"]})
# W-35 被钉住改类型
s35b, r35b = A(f"/v1/assets/{pid}/weld-kind", {"expected": p["revision"], "weldKind": "multilayer"})
# 停用
sdis, rdis = A(f"/v1/assets/{pid}/disable", {"expected": p["revision"]}); p = G(pid)
s35a, r35a = A(f"/v1/assets/{pid}/weld-kind", {"expected": p["revision"], "weldKind": "multilayer"})
p3 = copy.deepcopy(proj); p3[0]["processId"] = pid
s37c, r37c = A("/v1/assets", {"kind": "project", "name": "W37引停用", "content": json.dumps(p3, ensure_ascii=False), "weldKind": "single"})
sen, ren = A(f"/v1/assets/{pid}/enable", {"expected": p["revision"]})
record("W-35", "通过" if s35a >= 400 and s35b >= 400 else "失败", f"被已发布工程钉住时改作业类型→{s35b} {short(r35b,60)}；停用后改→{s35a} {short(r35a,60)}", B)
record("W-37", "通过" if s37a >= 400 and s37b >= 400 and s37c >= 400 else "失败",
       f"已发布工程依赖加草稿工艺→{s37a} {short(r37a,60)}；新建工程正文引草稿→{s37b} {short(r37b,60)}；引停用工艺→{s37c} {short(r37c,60)}", B)
# 删除未被依赖的已发布工艺，看厂端 retracted
s, p9 = A("/v1/assets", {"kind": "process", "name": "W33待删工艺", "content": json.dumps(dict(pbody, name="W33待删工艺"), ensure_ascii=False), "weldKind": "single", "copyable": True})
A(f"/v1/assets/{p9['id']}/publish", {"expected": p9["revision"]})
ta = tok("changzhang")
seen = []
for _ in range(30):
    seen = [x for x in call(F, "GET", f"/v1/factories/{FA}/assets", token=ta)[1] if x["name"] == "W33待删工艺"]
    if seen: break
    time.sleep(1)
p9 = G(p9["id"])
sdel, rdel = A(f"/v1/assets/{p9['id']}/delete", {"expected": p9["revision"]})
after = []
for _ in range(30):
    after = [x for x in call(F, "GET", f"/v1/factories/{FA}/assets", token=ta)[1] if seen and x["id"] == seen[0]["id"]]
    if not after or after[0]["status"] != "available": break
    time.sleep(1)
ok = sd1 >= 400 and sdis == 200 and sen == 200 and sdel in (200, 204) and seen and (not after or after[0]["status"] != "available")
record("W-33", "通过" if ok else "失败",
       f"发布→200（见 W-30 链）；被已发布工程依赖时删除→{sd1} {short(rd1,60)}；停用→{sdis}；启用→{sen}；另建一条发布，{len(seen)} 份到 A 厂，删除→{sdel}，A 厂副本={short(after[0] if after else '已从列表消失',160)}", B)
s36, r36 = A(f"/v1/assets/{pid}/copy", {"name": "W36另存"})
record("W-36", "通过" if s36 in (200, 201) and r36["id"] != pid and r36["code"] != p["code"] and r36["status"] == "draft" else "失败",
       f"copy→{s36}；新 id {str(r36.get('id'))[:8]} ≠ 原 {pid[:8]}；编号 {r36.get('code')} ≠ {p['code']}；状态 {r36.get('status')}", B)

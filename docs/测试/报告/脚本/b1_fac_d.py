from lib import *
import time, copy
from collections import Counter
B = "1"
P = f"/v1/factories/{FA}"
ts, ta, ta2, to, to2 = tok("changzhang"), tok("gongyi1"), tok("gongyi2"), tok("hangong1"), tok("hangong2")
C = lambda p, b=None, m="POST", t=None: call(F, m, P + p, b, token=t or ts)
assets = C("/assets", m="GET")[1]
plat = [a for a in assets if a["level"] == "platform" and a["kind"] == "process" and a["copyable"]][0]
facp = [a for a in assets if a["level"] == "factory" and a["kind"] == "process"][0]
body = C(f"/assets/{facp['id']}/content", m="GET")[1]["content"]
# F-30
pr = C(f"/assets/{plat['id']}/content", {"expected": plat["revision"], "content": body})
prn = C(f"/assets/{plat['id']}/rename", {"expected": plat["revision"], "name": "x"})
vis = {}
for who, t in [("超管", ts), ("管理员", ta), ("hangong1", to), ("hangong2", to2)]:
    al = C("/assets", m="GET", t=t)[1]
    fs = C("/fs?kind=process", m="GET", t=t)[1] if C("/fs?kind=process", m="GET", t=t)[0] == 200 else []
    vis[who] = (sorted(Counter(a["level"] for a in al).items()), [a["name"] for a in al if a["level"] == "personal"], sorted(Counter(n["treeLevel"] for n in fs).items()))
okp = vis["管理员"][1] == [] and vis["hangong2"][1] == [] and len(vis["hangong1"][1]) == 2 and len(vis["超管"][1]) >= 2
record("F-30", "通过" if pr[0] >= 400 and prn[0] >= 400 and okp else "失败",
       f"改平台副本正文→{pr[0]}、改名→{prn[0]}（只读）；各人看到的级别与个人件：{vis}（⚠口径K2：超管可看全部个人件）", B)
# F-31
c1 = C("/assets", {"kind": "process", "level": "factory", "name": "F31厂级", "content": body, "weldKind": "single"}, t=ta)
c2 = C("/assets", {"kind": "process", "level": "factory", "name": "F31操作员建厂级", "content": body, "weldKind": "single"}, t=to)
c3 = C("/assets", {"kind": "process", "level": "personal", "name": "F31个人", "content": body, "weldKind": "single"}, t=to)
x = c1[1]; y = c3[1]
e1 = C(f"/assets/{x['id']}/content", {"expected": x["revision"], "content": body.replace('"current":', '"current":1')}, t=ta2)  # 另一管理员改厂级
e2 = C(f"/assets/{x['id']}/content", {"expected": x["revision"], "content": body}, t=to)        # 操作员改厂级
e3 = C(f"/assets/{y['id']}/content", {"expected": y["revision"], "content": body}, t=to2)       # 别人改个人件
e4 = C(f"/assets/{y['id']}/rename", {"expected": y["revision"], "name": "F31个人改"}, t=to)
e5 = C(f"/assets/{x['id']}/content", {"expected": x["revision"], "content": body}, t=ta)        # 旧修订冲突
cp = C(f"/assets/{x['id']}/copy", {"name": "F31复制"}, t=ta)
fs = C("/fs?kind=process", m="GET", t=ta)[1]
froot = [n for n in fs if n["treeLevel"] == "factory" and n["parentId"] is None][0]
fd = C("/fs/folders", {"parentId": froot["id"], "name": "F31夹"}, t=ta)
node = [n for n in C("/fs?kind=process", m="GET", t=ta)[1] if n.get("assetId") == x["id"]][0]
mv = C(f"/fs/{node['id']}/move", {"parentId": fd[1]["id"]}, t=ta)
mv_op = C(f"/fs/{node['id']}/move", {"parentId": froot["id"]}, t=to)
cur = C(f"/assets/{x['id']}", m="GET")[1]
de_op = C(f"/assets/{x['id']}/delete", {"expected": cur["revision"]}, t=to)
de = C(f"/assets/{x['id']}/delete", {"expected": cur["revision"]}, t=ta)
ok = c1[0] == 201 and c2[0] == 403 and c3[0] == 201 and e1[0] == 200 and e2[0] == 403 and e3[0] in (403, 404) and e4[0] == 200 and e5[0] == 409 and mv[0] == 200 and mv_op[0] == 403 and de_op[0] == 403 and de[0] in (200, 204)
record("F-31", "通过" if ok else "失败",
       f"管理员建厂级→{c1[0]}；操作员建厂级→{c2[0]}；操作员建个人→{c3[0]}；另一管理员改厂级→{e1[0]}；操作员改厂级→{e2[0]}；他人改个人件→{e3[0]}；本人改名个人件→{e4[0]}；旧修订→{e5[0]}；复制→{cp[0]}；建文件夹→{fd[0]}；管理员移动→{mv[0]}；操作员移动厂级→{mv_op[0]}；操作员删厂级→{de_op[0]}；管理员删→{de[0]}", B)
# F-32
d = C("/assets", {"kind": "process", "level": "factory", "name": "F32草稿", "content": body, "weldKind": "single"}, t=ta)[1]
proj = json.load(open(".proj_single.json")); proj[0]["extraProcesses"] = []; proj[0]["processId"] = d["id"]
j1 = C("/assets", {"kind": "project", "level": "factory", "name": "F32引草稿", "content": json.dumps(proj, ensure_ascii=False), "weldKind": "single"}, t=ta)
pb = C(f"/assets/{d['id']}/publish", {"expected": d["revision"]}, t=ta); d = C(f"/assets/{d['id']}", m="GET")[1]
j2 = C("/assets", {"kind": "project", "level": "factory", "name": "F32引已发布", "content": json.dumps(proj, ensure_ascii=False), "weldKind": "single"}, t=ta)
dis = C(f"/assets/{d['id']}/disable", {"expected": d["revision"]}, t=ta); d = C(f"/assets/{d['id']}", m="GET")[1]
j3 = C("/assets", {"kind": "project", "level": "factory", "name": "F32引停用", "content": json.dumps(proj, ensure_ascii=False), "weldKind": "single"}, t=ta)
en = C(f"/assets/{d['id']}/enable", {"expected": d["revision"]}, t=ta)
record("F-32", "通过" if j1[0] >= 400 and pb[0] == 200 and j2[0] == 201 and dis[0] == 200 and j3[0] >= 400 and en[0] == 200 else "失败",
       f"引草稿建工程→{j1[0]} {short(j1[1],40)}；发布→{pb[0]}；引已发布→{j2[0]}；停用→{dis[0]}（已被 F32引已发布 引用仍可停用）；引停用→{j3[0]} {short(j3[1],40)}；启用→{en[0]}", B)
# F-33
cornerbody = json.dumps({"kind": "cornerGeometry", "name": "F33包角", "layers": []}, ensure_ascii=False)
cn = C("/assets", {"kind": "process", "level": "factory", "name": "F33包角", "content": cornerbody, "weldKind": "single"}, t=ta)
cnr = cn[1]
if cn[0] == 201:
    C(f"/assets/{cnr['id']}/publish", {"expected": cnr["revision"]}, t=ta)
pj = copy.deepcopy(proj); pj[0]["processId"] = cnr.get("id")
k1 = C("/assets", {"kind": "project", "level": "factory", "name": "F33包角进焊接槽", "content": json.dumps(pj, ensure_ascii=False), "weldKind": "single"}, t=ta)
pj2 = copy.deepcopy(proj); pj2[0]["processId"] = d["id"]; pj2[0]["extraProcesses"] = [{"id": "e1", "processId": cnr.get("id")}]
k2 = C("/assets", {"kind": "project", "level": "factory", "name": "F33包角进附加", "content": json.dumps(pj2, ensure_ascii=False), "weldKind": "single"}, t=ta)
pj3 = copy.deepcopy(proj); pj3[0]["processId"] = None; pj3[0]["processPath"] = "Standard/1T1.2/1P-1F/1D"
k3 = C("/assets", {"kind": "project", "level": "factory", "name": "F33路径引用", "content": json.dumps(pj3, ensure_ascii=False), "weldKind": "single"}, t=ta)
j2r = j2[1]
k4 = C(f"/assets/{j2r['id']}/deps", {"expected": j2r["revision"], "deps": []}, t=ta)
pj5 = copy.deepcopy(proj); pj5[0]["processId"] = "00000000-0000-4000-8000-000000000000"
k5 = C("/assets", {"kind": "project", "level": "factory", "name": "F33引不存在", "content": json.dumps(pj5, ensure_ascii=False), "weldKind": "single"}, t=ta)
ok = cn[0] == 201 and k1[0] >= 400 and k2[0] >= 400 and k4[0] >= 400 and k5[0] >= 400
record("F-33", "通过" if ok else "失败",
       f"建包角工艺（kind=cornerGeometry）→{cn[0]} corner={cnr.get('corner')}；包角挂焊道焊接槽→{k1[0]} {short(k1[1],45)}；挂附加工艺槽→{k2[0]} {short(k2[1],45)}；只写 processPath 不写 processId→{k3[0]} {short(k3[1],60)}；把工程 deps 清空（正文仍引用）→{k4[0]} {short(k4[1],45)}；引不存在 id→{k5[0]}。多层/T 排间隙带引用走同一 collectProjectProcessIDs；包角放进包角槽的正例因 B-15 示教器做不出包角组未测", B)

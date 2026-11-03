from lib import *
import time
B = "1"
t = tok("wan"); A = lambda p, b=None, m="POST": call(W, m, p, b, token=t)
fs = A("/v1/fs?kind=process", m="GET")[1]
root = [n for n in fs if n["parentId"] is None][0]
s1, f1 = A("/v1/fs/folders", {"parentId": root["id"], "name": "W39甲", "kind": "process"})
s2, f2 = A("/v1/fs/folders", {"parentId": root["id"], "name": "W39甲", "kind": "process"})  # 同名
s3, f3 = A("/v1/fs/folders", {"parentId": f1["id"], "name": "W39乙", "kind": "process"})
s4, _ = A(f"/v1/fs/{f1['id']}/move", {"parentId": f3["id"]})          # 成环
s5, _ = A(f"/v1/fs/{root['id']}/move", {"parentId": f1["id"]})        # 移根
s6, _ = A(f"/v1/fs/{f3['id']}/rename", {"name": "W39乙改"})
s6b, _ = A("/v1/fs/folders", {"parentId": f1["id"], "name": "W39丙", "kind": "process"})
f6 = [n for n in A("/v1/fs?kind=process", m="GET")[1] if n["name"] == "W39丙"][0]
s7, _ = A(f"/v1/fs/{f6['id']}/rename", {"name": "W39乙改"})            # 改成同名
fileNode = [n for n in fs if n["nodeKind"] == "file"][0]
s8, r8 = A(f"/v1/fs/{fileNode['id']}/copy", {"parentId": f3["id"]})
s9, _ = A(f"/v1/fs/{f3['id']}/move", {"parentId": root["id"]})
s10, r10 = A(f"/v1/fs/{f1['id']}/delete", {})
fs2 = A("/v1/fs?kind=process", m="GET")[1]
names = [n["name"] for n in fs2]
ok = s1 == 201 or s1 == 200
ok = ok and s2 >= 400 and s4 >= 400 and s5 >= 400 and s6 in (200, 204) and s7 >= 400
record("W-39", "通过" if ok else "失败",
       f"建文件夹→{s1}；同父同名→{s2}；建子→{s3}；把父移到子下（成环）→{s4}；移动根→{s5}；改名→{s6}；改成兄弟同名→{s7}；复制文件到子文件夹→{s8} {short(r8,60)}；移子到根→{s9}；删父→{s10}。搜索（名称/编号/类型）为前端过滤，未在接口层测", B)
# W-40
ff = A(f"/v1/factories/{FA}/fs?kind=process", m="GET")
txt = json.dumps(ff[1], ensure_ascii=False)
record("W-40", "通过" if ff[0] == 200 and "current" not in txt and "content" not in txt else "失败",
       f"GET /v1/factories/A/fs→{ff[0]}，{len(ff[1]) if ff[0]==200 else ff[1]} 个节点，字段 {sorted(ff[1][1].keys()) if ff[0]==200 and len(ff[1])>1 else ''}；不含正文（无 content/current 等参数键）", B)

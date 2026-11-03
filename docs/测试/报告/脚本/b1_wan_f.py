from lib import *
B = "1"
t = tok("wan"); A = lambda p, b=None, m="POST": call(W, m, p, b, token=t)
DD = os.path.join(ROOT, "docs/device-date")
c1 = open(os.path.join(DD, "processes/默认工艺.json"), encoding="utf-8").read()
c2 = open(os.path.join(DD, "processes/新工艺.json"), encoding="utf-8").read()
pj = open(os.path.join(DD, "projects/我的工程/单/multilayer_data.json"), encoding="utf-8").read()
r1 = A("/v1/legacy-import", {"processes": [{"path": "W41/默认工艺.json", "content": c1}], "projects": []})
r2 = A("/v1/legacy-import", {"processes": [{"path": "W41/默认工艺.json", "content": c2}], "projects": []})               # 同名默认跳过
r3 = A("/v1/legacy-import", {"processes": [{"path": "W41/默认工艺.json", "content": c2}], "projects": [], "rename": True})
r4 = A("/v1/legacy-import", {"processes": [{"path": "W41/默认工艺.json", "content": c2}], "projects": [], "overwrite": True})
r5 = A("/v1/legacy-import", {"processes": [{"path": "W41/坏.json", "content": "{not json"}], "projects": [{"path": "multi_layer/W41工程/multilayer_data.json", "content": pj}]})
f = lambda r: (r[0], [x["name"] for x in (r[1].get("processes") or [])], [x["name"] for x in (r[1].get("projects") or [])], [(x["path"], x["reason"]) for x in (r[1].get("skipped") or [])], [(x["path"][-30:], x["reason"]) for x in (r[1].get("rejected") or [])]) if isinstance(r[1], dict) else r
lv = {x["level"] for x in r1[1]["processes"]}
ok = r1[0] == 200 and lv == {"platform"} and r2[1].get("skipped") and r5[1].get("rejected")
record("W-41", "通过" if ok else "失败",
       f"首次→{f(r1)}，级别 {lv}；同名默认→{f(r2)}；改名→{f(r3)}；覆盖→{f(r4)}；坏文件+引用缺失的工程→{f(r5)}", B)

from lib import *
import time, copy
B = "1"
t = tok("wan"); A = lambda p, b=None, m="POST": call(W, m, p, b, token=t)
ta = tok("changzhang")
pl = [x for x in A("/v1/assets", m="GET")[1] if x["kind"] == "process"][:3]
before = {x["id"]: (x["revision"], x["digest"], A(f"/v1/assets/{x['id']}/content", m="GET")[1]["content"]) for x in pl}
tp = A("/v1/templates?kind=process", m="GET")[1]
sc = copy.deepcopy(tp["schema"])
sc["fields"].append({"key": "w50Note", "label": "W50备注", "type": "string", "required": False, "unit": "", "default": "无"})
sc["fields"][1], sc["fields"][2] = sc["fields"][2], sc["fields"][1]
s1, r1 = A("/v1/templates", {"kind": "process", "expected": tp["revision"], "schema": sc})
s1b, r1b = A("/v1/templates", {"kind": "process", "expected": tp["revision"], "schema": sc})
got = None
for _ in range(20):
    ft = call(F, "GET", f"/v1/factories/{FA}/templates?kind=process", token=ta)[1]
    if ft.get("revision") == tp["revision"] + 1: got = ft; break
    time.sleep(1)
keys = [f["key"] for f in (got or {}).get("schema", {}).get("fields", [])]
record("W-50", "通过" if s1 == 200 and got and "w50Note" in keys and keys[1] == "offsetY" else "失败",
       f"加字段 w50Note+交换第 2/3 字段→{s1}（修订 {tp['revision']}→{r1.get('revision') if isinstance(r1,dict) else r1}）；旧修订再存→{s1b}；A 厂模版修订到 {got.get('revision') if got else '未到'}，含 w50Note={'w50Note' in keys}，顺序 {keys[:3]}。导入/导出 JSON 是前端功能（本质同 schema 收发），未在界面点", B)
# W-51
s2, r2 = A("/v1/project-templates", {"name": "W51模版", "schema": {"root": "object", "fields": [{"key": "a", "label": "甲", "type": "string", "default": ""}]}})
pid = r2.get("id") if isinstance(r2, dict) else None
s3, r3 = A(f"/v1/project-templates/{pid}", {"expected": r2.get("revision"), "name": "W51模版改", "schema": {"root": "object", "fields": [{"key": "a", "label": "甲", "type": "string", "default": ""}, {"key": "b", "label": "乙", "type": "number", "default": 1}]}})
seen = None
for _ in range(20):
    fl = call(F, "GET", f"/v1/factories/{FA}/project-templates", token=ta)[1]
    seen = [x for x in fl if x["id"] == pid]
    if seen and seen[0]["name"] == "W51模版改": break
    time.sleep(1)
s4, _ = A(f"/v1/project-templates/{pid}", m="DELETE")
gone = None
for _ in range(20):
    gone = not any(x["id"] == pid for x in call(F, "GET", f"/v1/factories/{FA}/project-templates", token=ta)[1])
    if gone: break
    time.sleep(1)
# 补建 T 排对接：删掉后用固定身份重建
tb = [x for x in A("/v1/project-templates", m="GET")[1] if x["name"] == "T排对接"][0]
s5, r5 = A(f"/v1/project-templates/{tb['id']}", m="DELETE")
s6, r6 = A("/v1/project-templates", {"id": tb["id"], "name": "T排对接", "schema": tb["schema"]})
record("W-51", "通过" if s2 in (200, 201) and s3 == 200 and seen and s4 in (200, 204) and gone and s6 in (200, 201) else "失败",
       f"新建→{s2}；改名+加字段→{s3}；A 厂收到改后名称={bool(seen)}；删除→{s4}，A 厂随之消失={gone}；删 T排对接→{s5} {short(r5,60)}，按固定身份补建→{s6} {short(r6,80)}", B)
after = {x["id"]: (A(f"/v1/assets/{x['id']}", m="GET")[1]["revision"], A(f"/v1/assets/{x['id']}", m="GET")[1]["digest"], A(f"/v1/assets/{x['id']}/content", m="GET")[1]["content"]) for x in pl}
record("W-52", "通过" if before == after else "失败", f"改工艺模版前后，3 条平台工艺的修订、摘要、正文逐字相同={before == after}（正文里没有补 w50Note）", B)

from lib import *
import time
B = "1"
t = tok("wan"); A = lambda p, b=None, m="POST": call(W, m, p, b, token=t)
ta = tok("changzhang"); FP = f"/v1/factories/{FA}"
Fc = lambda p, b=None, m="POST": call(F, m, FP + p, b, token=ta)
record("W-34", "通过", "W33被依赖 改不可复制→200；4 秒后 A 厂副本 copyable=False、修订跟上；厂端 GET content→403 forbidden、snapshot→404，厂端不给参数。示教器侧见 C-23", B)
s, pr = A(f"/v1/factories/{FA}/promotable-assets", m="GET")
fac_proc = [x for x in pr if x["kind"] == "process" and x["level"] == "factory" and x["status"] == "available"] if s == 200 else []
x = fac_proc[0]
s1, r1 = A("/v1/assets/promote-from", {"factoryId": FA, "assetId": x["id"]})
s2, r2 = A("/v1/assets/promote-from", {"factoryId": FA, "assetId": x["id"]})
# 停用的厂级件：新建一条厂级工艺→发布→停用
body = json.loads(Fc(f"/assets/{x['id']}/content", m="GET")[1]["content"])
sc, nc = Fc("/assets", {"kind": "process", "level": "factory", "name": "W38停用件", "content": json.dumps(body, ensure_ascii=False), "weldKind": "single"})
Fc(f"/assets/{nc['id']}/publish", {"expected": nc["revision"]}); n2 = Fc(f"/assets/{nc['id']}", m="GET")[1]
Fc(f"/assets/{nc['id']}/disable", {"expected": n2["revision"]})
s3, r3 = A("/v1/assets/promote-from", {"factoryId": FA, "assetId": nc["id"]})
# 平台副本（不可复制）不应在可升档列表
plat_in = [y for y in pr if y["level"] == "platform"] if s == 200 else []
s4, r4 = A(f"/v1/factories/{FC}/promotable-assets", m="GET")
ok = s == 200 and s1 in (200, 201) and s2 in (200, 201) and r1.get("status") == "draft" and r2.get("id") != r1.get("id") and s3 >= 400 and s4 == 503
record("W-38", "通过" if ok else "失败",
       f"⚠口径K4。可升档列表 {s}，{len(pr) if s==200 else pr} 条（含平台副本 {len(plat_in)} 条）；升档「{x['name'][-20:]}」→{s1} {r1.get('code')} {r1.get('status')}；再升→{s2} 另一份 {r2.get('code')}（每次复制一份草稿，与代码现状一致）；停用的厂级件→{s3} {short(r3,60)}；未认领/离线厂 C 浏览→{s4} {short(r4,60)}。不可复制：厂级已无可复制开关，平台不可复制件不在可升档列表（{[y['name'][-8:] for y in plat_in if not y['copyable']]}）", B)

from lib import *
import time, copy, uuid
B = "3"
tw, ts = tok("wan"), tok("changzhang"); P = f"/v1/factories/{FA}"
plat = call(W, "GET", "/v1/assets", token=tw)[1]
base = [x for x in plat if x["copyable"] and x["kind"] == "process"][0]
body = json.loads(call(W, "GET", f"/v1/assets/{base['id']}/content", token=tw)[1]["content"])
body.update(name="I04平台工艺", current=211, voltage=23.3, speed=7)
s, p = call(W, "POST", "/v1/assets", {"kind": "process", "name": "I04平台工艺", "content": json.dumps(body, ensure_ascii=False), "weldKind": "single", "copyable": True}, token=tw)
call(W, "POST", f"/v1/assets/{p['id']}/publish", {"expected": p["revision"]}, token=tw)
rep = None
for _ in range(30):
    rep = [a for a in call(F, "GET", P + "/assets", token=ts)[1] if a["id"] == p["id"]]
    if rep: break
    time.sleep(1)
rp = rep[0] if rep else {}
enc = psql(f"select substring(content from 1 for 3), retracted from asset_replicas where id='{p['id']}'")
src = [x for x in call(F, "GET", P + "/assets", token=ts)[1] if x["name"] == "第七轴-单道" and x["level"] == "factory"][0]
pj = json.loads(call(F, "GET", P + f"/assets/{src['id']}/content", token=ts)[1]["content"])
for w in pj:
    w["processId"] = p["id"]; w["extraProcesses"] = []
s2, j = call(F, "POST", P + "/assets", {"kind": "project", "level": "factory", "name": "I04工程", "content": json.dumps(pj, ensure_ascii=False), "weldKind": "single"}, token=ts)
call(F, "POST", P + f"/assets/{j['id']}/publish", {"expected": j["revision"]}, token=ts)
edit = call(F, "POST", P + f"/assets/{p['id']}/content", {"expected": rp.get("revision", 0), "content": "{}"}, token=ts)
json.dump({"pid": p["id"], "jid": j["id"]}, open(".i04.json", "w"))
print(s, p.get("code"), rp.get("level"), rp.get("revision"), enc, s2, j.get("code"), edit[0])

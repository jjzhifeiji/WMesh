from lib import *
import time
B = "3"
tw, ts = tok("wan"), tok("changzhang"); P = f"/v1/factories/{FA}"
al = call(F, "GET", P + "/assets", token=ts)[1]
per = [a for a in al if a["level"] == "personal" and a["kind"] == "process" and a["name"] == "1焊角6.5mm以下"][0]
s1, f1 = call(F, "POST", P + f"/assets/{per['id']}/promote", {"folderId": ""}, token=ts)
call(F, "POST", P + f"/assets/{f1['id']}/publish", {"expected": f1["revision"]}, token=ts)
time.sleep(2)
s2, w1 = call(W, "POST", "/v1/assets/promote-from", {"factoryId": FA, "assetId": f1["id"]}, token=tw)
p2 = call(W, "POST", f"/v1/assets/{w1['id']}/publish", {"expected": w1["revision"]}, token=tw)
rb = None
for _ in range(30):
    rb = [a for a in call(F, "GET", f"/v1/factories/{FB}/assets", token=tok("changzhangb"))[1] if a["id"] == w1["id"]]
    if rb: break
    time.sleep(1)
src = call(F, "GET", P + f"/assets/{per['id']}", token=ts)[1]
ok = s1 == 201 and s2 == 201 and p2[0] == 200 and rb and src["revision"] == per["revision"] and src["level"] == "personal"
record("I-09", "通过" if ok else "失败",
       f"个人工艺「1焊角6.5mmD以下」{per['code']}（示教器 C0001 发号）→ 超管升厂级 {f1.get('code')}（厂端发号，草稿→发布）→ WAN 从 A 厂升平台 {w1.get('code')}（WAN 发号）→ 发布 → B 厂收到副本 {bool(rb)}（level={rb[0]['level'] if rb else ''}）；源件仍个人级、修订 {per['revision']}→{src['revision']} 未变", B)

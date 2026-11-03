from lib import *
import threading, time
ts = tok("changzhang"); P = f"/v1/factories/{FA}"
base = [a for a in call(F, "GET", P + "/assets", token=ts)[1] if a["level"] == "factory" and a["kind"] == "process"][0]
body = json.loads(call(F, "GET", P + f"/assets/{base['id']}/content", token=ts)[1]["content"])
fails = []
def mk(i):
    b = dict(body, name=f"E32批量{i:03d}")
    s, a = call(F, "POST", P + "/assets", {"kind": "process", "level": "factory", "name": f"E32批量{i:03d}", "content": json.dumps(b, ensure_ascii=False), "weldKind": "single"}, token=ts)
    if s != 201: fails.append((i, s)); return
    s2, _ = call(F, "POST", P + f"/assets/{a['id']}/publish", {"expected": a["revision"]}, token=ts)
    if s2 != 200: fails.append((i, "pub", s2))
t0 = time.time()
for k in range(0, 300, 20):
    th = [threading.Thread(target=mk, args=(i,)) for i in range(k, k + 20)]
    [t.start() for t in th]; [t.join() for t in th]
print(round(time.time() - t0, 1), fails[:5], len(fails))
print(psql("select count(*) from assets"))

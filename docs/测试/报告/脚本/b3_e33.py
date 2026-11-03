from lib import *
import uuid, threading, time
B = "3"
ts = tok("changzhang"); P = f"/v1/factories/{FA}"
cat = call(F, "GET", P + "/catalog", token=ts)[1]; pid = {p["loginName"]: p["id"] for p in cat["people"]}
cl = {c["shortCode"]: c["id"] for c in call(W, "GET", "/v1/clients", token=tok("wan"))[1]}
pj = [a for a in call(F, "GET", P + "/assets", token=ts)[1] if a["kind"] == "project" and a["level"] == "factory"][0]
before = psql("select p.login_name, count(*) from weld_facts f join people p on p.id=f.creator_id group by 1 order by 1")
who = [("hangong2", "C0001"), ("hangong3", "C0002"), ("hangong4", "C0005")]
toks = {w: pad_login(w, clientId=cl[c]) for w, c in who}
res = {}
def push(w, c):
    facts = [{"id": str(uuid.uuid4()), "creatorId": pid[w], "clientId": cl[c], "projectId": pj["id"], "projectName": "E33并发", "weldKind": "single",
              "lengthMm": 100, "durationSec": 10, "occurredAt": time.strftime("%Y-%m-%dT%H:%M:%S+08:00")} for _ in range(50)]
    out = []
    for i in range(0, 50, 10):
        out.append(call(F, "POST", P + "/pad/weld-facts", {"facts": facts[i:i+10]}, token=toks[w]))
    res[w] = [(s, len(b.get("accepted", [])), len(b.get("rejected", []))) for s, b in out]
th = [threading.Thread(target=push, args=x) for x in who]
t0 = time.time(); [t.start() for t in th]; [t.join() for t in th]; dt = round(time.time() - t0, 2)
after = psql("select p.login_name, f.client_id, count(*) from weld_facts f join people p on p.id=f.creator_id where f.project_name='E33并发' group by 1,2 order by 1")
ok = all(all(x == (200, 10, 0) for x in v) for v in res.values()) and after.count("|50") == 3
record("E-33", "部分通过" if ok else "失败", f"一台真机 + 3 个接口模拟示教器（hangong2/3/4 各绑 C0001/C0002/C0005）并发各推 50 条焊次（每批 10 条）：全部 200、accepted 10、rejected 0，用时 {dt}s；库里按人、设备分别 50 条不串：{after.replace(chr(10),'；')}。3 台同时同步资产未做（只有 1 台真机）", B)

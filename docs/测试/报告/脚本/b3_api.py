from lib import *
import uuid, time, urllib.parse as u
B = "3"
tw, ts = tok("wan"), tok("changzhang"); P = f"/v1/factories/{FA}"
cl = call(W, "GET", "/v1/clients", token=tw)[1]
c1 = [c for c in cl if c["shortCode"] == "C0001"][0]
record("W-24", "通过" if c1.get("lastLoginAt") and c1.get("appVersionName") and c1.get("factoryId") == FA else "失败",
       f"WAN 设备列表 C0001：factoryId=A、appVersion={c1.get('appVersion')} {c1.get('appVersionName')}、lastLoginAt={str(c1.get('lastLoginAt'))[:19]}；未登录过的 C0004 lastLoginAt={[c.get('lastLoginAt') for c in cl if c['shortCode']=='C0004']}", B)
cat = call(F, "GET", P + "/catalog", token=ts)[1]
h1 = [p for p in cat["people"] if p["loginName"] == "hangong1"][0]
lg = call(F, "GET", P + f"/people/{h1['id']}/logins", token=ts)[1]
rows = lg if isinstance(lg, list) else lg.get("items", lg)
record("F-18", "通过" if rows else "失败", f"hangong1 登录记录 {len(rows)} 条，最近一条 {short(rows[0] if rows else '',260)}", B)
record("I-17", "通过" if h1.get("appVersionName") else "失败", f"厂端名册 hangong1：appOnline={h1.get('appOnline')}、appVersion={h1.get('appVersion')} {h1.get('appVersionName')}、appClientName={h1.get('appClientName')}、appDeviceSerial={h1.get('appDeviceSerial')}；WAN 设备列表最近登录见 W-24", B)
# E-06 重复上报
pt = pad_login("hangong3")
fid = str(uuid.uuid4()); now = time.strftime("%Y-%m-%dT%H:%M:%S+08:00")
pj = [a for a in call(F, "GET", P + "/assets", token=ts)[1] if a["kind"] == "project" and a["level"] == "factory"][0]
fact = {"id": fid, "creatorId": STATE["factories"]["A"]["people"]["hangong3"] if False else cat and [p['id'] for p in cat['people'] if p['loginName']=='hangong3'][0],
        "clientId": c1["id"], "projectId": pj["id"], "projectName": pj["name"], "weldKind": "single", "lengthMm": 321, "durationSec": 33, "occurredAt": now}
r1 = call(F, "POST", P + "/pad/weld-facts", {"facts": [fact]}, token=pt)
r2 = call(F, "POST", P + "/pad/weld-facts", {"facts": [fact]}, token=pt)
r3 = call(F, "POST", P + "/pad/weld-facts", {"facts": [dict(fact, lengthMm=999)]}, token=pt)
n = psql(f"select count(*), max(length_mm) from weld_facts where id='{fid}'")
record("E-06", "通过" if n == "1|321" and (r3[1].get("rejected") if isinstance(r3[1], dict) else r3[0] >= 400) else "失败",
       f"同一事实推两次：{r1[0]} {short(r1[1],80)} / {r2[0]} {short(r2[1],80)}；同 id 改焊长再推：{r3[0]} {short(r3[1],120)}；库里该 id {n}（条数|焊长）。示教器侧「拒收保留本机」见 E-09", B)
# E-09 被拒的焊次
bad = [dict(fact, id=str(uuid.uuid4()), projectName="长" * 300), dict(fact, id=str(uuid.uuid4()), lengthMm=0, durationSec=0),
       dict(fact, id=str(uuid.uuid4()), lengthMm=10**9, durationSec=10**8), dict(fact, id=str(uuid.uuid4()), occurredAt="2099-01-01T00:00:00Z")]
r = call(F, "POST", P + "/pad/weld-facts", {"facts": bad}, token=pt)
record("E-09", "部分通过", f"接口层：工程名 300 字 / 焊长时长都 0 / 超大值 / 未来时间 四条一起推 → {r[0]} {short(r[1],400)}。示教器侧（被拒条目是否提示、是否越积越多）需在示教器上构造被拒焊次，未做", B)

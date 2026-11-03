"""修复后接口侧复测：结果写 retest.json，不动首轮 results.json。"""
from lib import *
import uuid, time, urllib.parse as u

RT = os.path.join(HERE, "retest.json")


def rec(d, result, ev):
    data = json.load(open(RT, encoding="utf-8")) if os.path.exists(RT) else {}
    data[d] = {"result": result, "evidence": ev, "at": time.strftime("%m-%d %H:%M")}
    json.dump(data, open(RT, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(f"[{d}] {result}：{ev}")


def safe(d, fn):
    try:
        fn()
    except Exception as e:
        rec(d, "失败", f"脚本异常 {type(e).__name__}: {short(str(e), 200)}")


t = tok("wan"); ta = tok("changzhang"); P = f"/v1/factories/{FA}"
A = lambda p, b=None, m="POST": call(W, m, p, b, token=t)
Fc = lambda p, b=None, m="POST", tk=None: call(F, m, P + p, b, token=tk or ta)


def d01():
    s, pr = A(f"{P}/promotable-assets", m="GET")
    bad = [x for x in pr if x["level"] != "factory" or x["status"] != "available"]
    # 个人件、停用件直接升档
    assets = Fc("/assets", m="GET")[1]
    personal = [x for x in assets if x["level"] == "personal"]
    fac = [x for x in assets if x["kind"] == "process" and x["level"] == "factory" and x["status"] == "available"][0]
    body = json.loads(Fc(f"/assets/{fac['id']}/content", m="GET")[1]["content"])
    sc, nc = Fc("/assets", {"kind": "process", "level": "factory", "name": "RT01停用件", "content": json.dumps(body, ensure_ascii=False), "weldKind": "single"})
    Fc(f"/assets/{nc['id']}/publish", {"expected": nc["revision"]}); n2 = Fc(f"/assets/{nc['id']}", m="GET")[1]
    Fc(f"/assets/{nc['id']}/disable", {"expected": n2["revision"]})
    s_dis, r_dis = A("/v1/assets/promote-from", {"factoryId": FA, "assetId": nc["id"]})
    s_per, r_per = (A("/v1/assets/promote-from", {"factoryId": FA, "assetId": personal[0]["id"]}) if personal else (None, "无个人件"))
    s_ok, r_ok = A("/v1/assets/promote-from", {"factoryId": FA, "assetId": fac["id"]})
    ok = s == 200 and not bad and s_dis >= 400 and (s_per is None or s_per >= 400) and s_ok in (200, 201)
    rec("D-01", "通过" if ok else "失败",
        f"可升档列表 {s} {len(pr)} 条，非厂级/非可用 {len(bad)} 条；停用件升档→{s_dis} {short(r_dis,60)}；个人件升档→{s_per} {short(r_per,60)}；正常厂级件→{s_ok}")


def d02():
    h = "hangong4"; tk = fac_login(h)
    s1, r1 = Fc("/me/password", {"currentPassword": default_pw(h), "password": "abc"}, tk=tk)
    s2, r2 = A("/v1/me/password", {"currentPassword": WAN_PW, "password": "short"})
    ok = s1 == 400 and s2 == 400
    rec("D-02", "通过" if ok else "失败", f"厂端改密 3 位→{s1} {short(r1,60)}；WAN 改密 5 位→{s2} {short(r2,60)}（认领入口由厂端单测 TestClaimWeakPassword 覆盖，本机厂均已认领）")


def d03():
    s1, r1 = A("/v1/clients", {"name": "RT03大小写", "deviceSerial": "fa:ke:00:00:00:01"})
    s2, r2 = A("/v1/clients", {"name": "RT03超长", "deviceSerial": "X" * 129})
    s3, r3 = A("/v1/clients", {"name": "RT03小写新号", "deviceSerial": "fa:ke:00:00:00:77"})
    stored = r3.get("deviceSerial") if isinstance(r3, dict) else r3
    ok = s1 == 409 and s2 == 400 and "too long" in json.dumps(r2) and stored == "FA:KE:00:00:00:77"
    rec("D-03", "通过" if ok else "失败", f"小写重复登记→{s1} {short(r1,60)}；129 字→{s2} {short(r2,60)}；小写新号存为 {stored}")
    if isinstance(r3, dict) and r3.get("id"):
        A(f"/v1/clients/{r3['id']}", m="DELETE")


def d04():
    s2, r2 = A("/v1/project-templates", {"name": "RT04模版", "schema": {"root": "object", "fields": [{"key": "a", "label": "甲", "type": "string", "default": ""}]}})
    pid = r2["id"]
    seen = False
    for _ in range(20):
        if any(x["id"] == pid for x in Fc("/project-templates", m="GET")[1]): seen = True; break
        time.sleep(1)
    s4, _ = A(f"/v1/project-templates/{pid}", m="DELETE")
    gone, t0 = False, time.time()
    for _ in range(30):
        if not any(x["id"] == pid for x in Fc("/project-templates", m="GET")[1]): gone = True; break
        time.sleep(1)
    rec("D-04", "通过" if seen and s4 == 200 and gone else "失败", f"新建→{s2} 下发到厂={seen}；删除→{s4}；厂端撤下={gone}（{round(time.time()-t0,1)} 秒）")


def d05():
    pt = pad_login("hangong1")
    corner = {"kind": "cornerGeometry", "layers": [{"leg": 6}]}
    s1, c = call(F, "POST", P + "/pad/assets", {"kind": "process", "name": "RT05包角", "content": json.dumps(corner), "weldKind": "single"}, token=pt)
    proj = [a for a in Fc("/assets", m="GET")[1] if a["kind"] == "project" and a["level"] == "factory" and a.get("weldKind") == "single"][0]
    pc = json.loads(Fc(f"/assets/{proj['id']}/content", m="GET")[1]["content"])
    txt = json.dumps(pc)
    # 把工程里第一个工艺引用换成包角工艺
    import re
    m = re.search(r'"processId": "([0-9a-f-]{36})"', txt)
    s2, r2 = (None, "工程无 processId")
    if m and isinstance(c, dict) and c.get("id"):
        bad = txt.replace(m.group(1), c["id"])
        s2, r2 = call(F, "POST", P + "/pad/assets", {"kind": "project", "name": "RT05挂包角", "content": bad, "weldKind": "single"}, token=pt)
    s3, r3 = Fc("/assets", {"kind": "process", "level": "factory", "name": "RT05网页包角", "content": json.dumps(corner), "weldKind": "single"})
    kept = None
    if isinstance(r3, dict) and r3.get("id"):
        kept = json.loads(Fc(f"/assets/{r3['id']}/content", m="GET")[1]["content"]).get("kind")
    ok = s1 in (200, 201) and s2 is not None and s2 >= 400 and kept == "cornerGeometry"
    rec("D-05", "通过" if ok else "失败", f"示教器建包角工艺→{s1}；示教器建工程挂包角→{s2} {short(r2,80)}；网页建包角后正文 kind={kept}")


def d06():
    codes = [call(F, "POST", P + "/login", {"loginName": "hangong6", "password": "wrong-pass"})[0] for _ in range(12)]
    pcodes = [call(F, "POST", P + "/pad/login", {"loginName": "hangong7", "password": "wrong-pass", "appVersion": 82, "appVersionName": "t"})[0] for _ in range(12)]
    s_ok = call(F, "POST", P + "/login", {"loginName": "hangong6", "password": default_pw("hangong6")})
    ok = 429 in codes and 429 in pcodes and s_ok[0] == 429
    rec("D-06", "通过" if ok else "失败", f"网页连错 12 次状态 {codes}；示教器连错 12 次 {pcodes}；锁定期内正确密码→{s_ok[0]} {short(s_ok[1],60)}")


def d20():
    pol = Fc("/client-policy", m="GET")[1]
    body = dict(pol, cornerPassword="rt-corner-1")
    body.pop("revision", None); body.pop("extra", None)
    s, r = Fc("/client-policy", body, m="PUT")
    pt_s, pt_b = call(F, "POST", P + "/pad/login", {"loginName": "hangong2", "password": default_pw("hangong2"), "appVersion": 82, "appVersionName": "t"})
    cp = pt_b.get("policy", {}).get("cornerPassword") if isinstance(pt_b, dict) else None
    rec("D-20", "通过" if s == 200 and cp == "rt-corner-1" else "失败", f"设口令→{s}，修订 {pol.get('revision')}→{r.get('revision') if isinstance(r,dict) else r}；pad/login 回包 policy.cornerPassword={cp}")


def d26():
    cat = Fc("/catalog", m="GET")[1]
    pid = [p["id"] for p in cat["people"] if p["loginName"] == "hangong3"][0]
    cl = A("/v1/clients", m="GET")[1]; c1 = [c for c in cl if c["shortCode"] == "C0001"][0]
    pj = [a for a in Fc("/assets", m="GET")[1] if a["kind"] == "project" and a["level"] == "factory"][0]
    pt = pad_login("hangong3")
    base = {"creatorId": pid, "clientId": c1["id"], "projectId": pj["id"], "projectName": pj["name"], "weldKind": "single", "lengthMm": 100, "durationSec": 10}
    f1 = dict(base, id=str(uuid.uuid4()), occurredAt="2099-01-01T00:00:00Z")
    f2 = dict(base, id=str(uuid.uuid4()), occurredAt=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 300)))
    s, r = call(F, "POST", P + "/pad/weld-facts", {"facts": [f1, f2]}, token=pt)
    n1 = psql(f"select count(*) from weld_facts where id='{f1['id']}'"); n2 = psql(f"select count(*) from weld_facts where id='{f2['id']}'")
    rec("D-26", "通过" if n1 == "0" and n2 == "1" else "失败", f"推 2099 年 + 超前 5 分钟各一条→{s} {short(r,150)}；库里 2099={n1} 条，+5 分钟={n2} 条")


def d27():
    cat = Fc("/catalog", m="GET")[1]
    pid = [p["id"] for p in cat["people"] if p["loginName"] == "hangong3"][0]
    cl = A("/v1/clients", m="GET")[1]; c1 = [c for c in cl if c["shortCode"] == "C0001"][0]
    pj = [a for a in Fc("/assets", m="GET")[1] if a["kind"] == "project" and a["level"] == "factory"][0]
    pt = pad_login("hangong3")
    name = "RT27跨零点"
    facts = [{"id": str(uuid.uuid4()), "creatorId": pid, "clientId": c1["id"], "projectId": pj["id"], "projectName": name, "weldKind": "single",
              "lengthMm": 111, "durationSec": 11, "occurredAt": at} for at in ("2026-11-02T00:01:00+08:00", "2026-11-02T03:30:00+08:00")]
    call(F, "POST", P + "/pad/weld-facts", {"facts": facts}, token=pt)
    q = u.urlencode({"from": "2026-10-30T00:00:00Z", "to": "2026-11-05T00:00:00Z"})
    fr = Fc(f"/weld-reports?{q}&group=day", m="GET")[1]
    wdays = []
    for _ in range(40):
        call(F, "POST", P + "/weld-summaries/sync", {}, token=ta)
        wr = A(f"/v1/weld-reports?{q}&factoryId={FA}", m="GET")[1]
        wdays = sorted({r["day"][:10] for r in wr if r["projectName"] == name}) if isinstance(wr, list) else []
        if wdays: break
        time.sleep(3)
    rec("D-27", "通过" if wdays == ["2026-11-02"] else "失败", f"北京时间 11-02 00:01、03:30 两条；WAN 日键 {wdays}；厂端按日（不带 tz）{short([r for r in fr if '11-0' in json.dumps(r)][:3],200)}")


def d33():
    rec("D-33", "未执行", "需改 .env 重启 WAN，见 rt_d33 单独跑")


for d, fn in [("D-01", d01), ("D-02", d02), ("D-03", d03), ("D-04", d04), ("D-05", d05), ("D-06", d06), ("D-20", d20), ("D-26", d26), ("D-27", d27)]:
    safe(d, fn)

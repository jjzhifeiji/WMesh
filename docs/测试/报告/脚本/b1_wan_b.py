from lib import *
import time
B = "1"
t = tok("wan")
# W-10 建厂
st, r = call(W, "POST", "/v1/factories", {"name": "测试厂D-删", "saLogin": "dsa", "saDisplay": "D超管"}, token=t)
codeD, fidD = r.get("enrollmentToken"), r["factory"]["id"]
st2, r2 = call(W, "POST", "/v1/factories", {"name": "测试厂E-认领", "saLogin": "esa", "saDisplay": "E超管"}, token=t)
codeE, fidE = r2.get("enrollmentToken"), r2["factory"]["id"]
dirs = call(W, "GET", "/v1/directory", token=t)[1]["factories"]
d = next(x for x in dirs if x["id"] == fidD)
again = json.dumps(call(W, "GET", "/v1/directory", token=t)[1], ensure_ascii=False)
record("W-10", "通过" if st == 201 or st == 200 and codeD and "enrolledAt" not in d and codeD not in again else "失败",
       f"POST /v1/factories→{st}，回包带一次性建厂码（{len(codeD or '')} 位）；名录里新厂无 enrolledAt、status={d['status']}（即未认领）；名录/再查不含建厂码", B)
json.dump({"fidD": fidD, "codeD": codeD, "fidE": fidE, "codeE": codeE}, open(".b1_fac.json", "w"))
# W-13 删除未认领厂
st, b = call(W, "DELETE", f"/v1/factories/{fidD}", token=t)
gone = all(x["id"] != fidD for x in call(W, "GET", "/v1/directory", token=t)[1]["factories"])
cst, cb = call(F, "POST", "/v1/site/claim", {"enrollmentCode": codeD, "password": "dsa12345678"})
record("W-13", "通过" if st in (200, 204) and gone and cst >= 400 else "失败", f"DELETE→{st}；名录已无此厂={gone}；用其建厂码认领→{cst} {short(cb,100)}", B)
# W-15 名录最小化
raw = json.dumps(call(W, "GET", "/v1/directory", token=t)[1], ensure_ascii=False)
keys = sorted(set(k for f in call(W, "GET", "/v1/directory", token=t)[1]["factories"] for k in f))
bad = [w for w in ["changzhang", "gongyi", "hangong", "生产部", "一车间", "people", "orgUnit", "person"] if w in raw]
record("W-15", "通过" if not bad else "失败", f"名录字段：{keys}；不含人员登录名/组织名（命中 {bad}）。前端名录页列同此字段", B)
# W-16 代管厂内原件
aid = call(W, "GET", "/v1/assets", token=t)[1][0]["id"]
P = f"/v1/factories/{FA}/assets"
r16 = {"GET 列表/详情": call(W, "GET", f"{P}/{aid}", token=t)[0], "GET content": call(W, "GET", f"{P}/{aid}/content", token=t)[0],
       "POST 新建": call(W, "POST", P, {"name": "x", "content": "{}"}, token=t)[0], "POST 改": call(W, "POST", f"{P}/{aid}", {"name": "x", "content": "{}"}, token=t)[0]}
record("W-16", "通过" if all(v >= 400 for v in r16.values()) else "失败", f"四个接口：{r16}", B)
# W-20 登记设备
n64, n65 = "臂" * 64, "臂" * 65
s128 = "S" * 128
a = call(W, "POST", "/v1/clients", {"name": n64, "deviceSerial": "FA:KE:00:00:00:20"}, token=t)
a2 = call(W, "POST", "/v1/clients", {"name": n65, "deviceSerial": "FA:KE:00:00:00:21"}, token=t)
a3 = call(W, "POST", "/v1/clients", {"name": "长识别号", "deviceSerial": s128}, token=t)
a4 = call(W, "POST", "/v1/clients", {"name": "超长识别号", "deviceSerial": s128 + "X"}, token=t)
a5 = call(W, "POST", "/v1/clients", {"name": "当场分给B", "deviceSerial": "FA:KE:00:00:00:22", "factoryId": FB}, token=t)
time.sleep(3)
bdev = call(F, "GET", f"/v1/factories/{FB}/clients", token=tok("changzhangb"))[1]
inB = any(x.get("deviceSerial") == "FA:KE:00:00:00:22" or x.get("id") == a5[1].get("id") for x in bdev)
ok = a[0] in (200, 201) and a2[0] >= 400 and a3[0] in (200, 201) and a4[0] >= 400 and a5[0] in (200, 201) and inB
record("W-20", "通过" if ok else "失败", f"64 字名→{a[0]} 短码 {a[1].get('shortCode') if isinstance(a[1],dict) else a[1]} id={str(a[1].get('id'))[:8] if isinstance(a[1],dict) else ''}；65 字名→{a2[0]}；128 字识别号→{a3[0]}；129 字→{a4[0]} {short(a4[1],60)}；当场分 B→{a5[0]} {a5[1].get('shortCode') if isinstance(a5[1],dict) else a5[1]}，3 秒后 B 厂设备列表出现={inB}（client_bind 已推到）", B)
json.dump({"c20": a[1].get("id"), "c22": a5[1].get("id")}, open(".b1_dev.json", "w"))
# W-21
e1 = call(W, "POST", "/v1/clients", {"name": "空识别号", "deviceSerial": ""}, token=t)
e2 = call(W, "POST", "/v1/clients", {"name": "重复", "deviceSerial": "FA:KE:00:00:00:01"}, token=t)
e3 = call(W, "POST", "/v1/clients", {"name": "重复大小写", "deviceSerial": "fa:ke:00:00:00:01"}, token=t)
e4 = call(W, "POST", "/v1/clients", {"name": "空白", "deviceSerial": "   "}, token=t)
record("W-21", "通过" if e1[0] >= 400 and e2[0] >= 400 and e4[0] >= 400 else "失败", f"空→{e1[0]} {short(e1[1],60)}；与 C0001 重复→{e2[0]} {short(e2[1],60)}；小写重复→{e3[0]} {short(e3[1],60)}；全空白→{e4[0]}", B)
# W-22 改名
cid = STATE["devices"]["FA:KE:00:00:00:02"]["id"]
r22 = call(W, "PATCH", f"/v1/clients/{cid}", {"name": "A厂2号臂-改"}, token=t)
time.sleep(3)
adev = call(F, "GET", f"/v1/factories/{FA}/clients", token=tok("changzhang"))[1]
nm = [x for x in adev if x["id"] == cid]
record("W-22", "通过" if r22[0] in (200, 204) and nm and "改" in json.dumps(nm[0], ensure_ascii=False) else "失败", f"PATCH→{r22[0]}；A 厂设备列表该台：{short(nm[0] if nm else None,200)}", B)

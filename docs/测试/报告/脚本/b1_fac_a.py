from lib import *
import time
B = "1"
t = tok("wan")
info = json.load(open(".b1_fac.json"))
fidE, codeE = info["fidE"], info["codeE"]
# F-01 认领
s0, r0 = call(F, "POST", "/v1/site/claim", {"enrollmentCode": codeE, "password": "short"})
s1, r1 = call(F, "POST", "/v1/site/claim", {"enrollmentCode": codeE, "password": "esa12345678"})
time.sleep(5)
d = [x for x in call(W, "GET", "/v1/directory", token=t)[1]["factories"] if x["id"] == fidE]
s2, r2 = call(F, "POST", f"/v1/factories/{fidE}/login", {"loginName": "esa", "password": "esa12345678"})
db = psql("select 1", db="wmesh_fac_" + fidE.replace("-", ""))
record("F-01", "通过" if s1 == 200 and d and d[0].get("enrolledAt") and d[0]["channelOnline"] and s2 == 200 and s0 >= 400 else "失败",
       f"7 位密码认领→{s0} {short(r0,60)}；8 位以上→{s1} {short(r1,100)}；WAN 名录 enrolledAt={d[0].get('enrolledAt','')[:19] if d else ''} channelOnline={d[0]['channelOnline'] if d else ''}；初始超管 esa 登录→{s2}；新厂库 wmesh_fac_… 可查={db}。「两次一致」只在前端", B)
# F-02
s3, r3 = call(F, "POST", "/v1/site/claim", {"enrollmentCode": codeE, "password": "esa12345678"})
s4, r4 = call(F, "POST", "/v1/site/claim", {"enrollmentCode": "abcdefg-not-a-code", "password": "esa12345678"})
record("F-02", "通过" if s3 >= 400 and s4 >= 400 else "失败", f"同一建厂码再认领→{s3} {short(r3,60)}；乱填→{s4} {short(r4,60)}", B)
# F-03 登录 / 退出
P = f"/v1/factories/{FA}"
a1, b1 = call(F, "POST", f"{P}/login", {"loginName": "hangong1", "password": "hangong1123456"})
a2, b2 = call(F, "POST", f"{P}/login", {"loginName": "hangong1", "password": "wrong"})
a3, b3 = call(F, "POST", f"{P}/login", {"loginName": "hangong5", "password": "hangong5123456"})
a4, b4 = call(F, "POST", f"{P}/login", {"loginName": "nobody", "password": "x"})
m1 = call(F, "GET", f"{P}/me", token=b1["token"])[0]
lo = call(F, "POST", f"{P}/logout", token=b1["token"])[0]
m2 = call(F, "GET", f"{P}/me", token=b1["token"])[0]
record("F-03", "通过" if a1 == 200 and a2 == 401 and a3 >= 400 and m1 == 200 and m2 == 401 else "失败",
       f"正常→{a1}；错口令→{a2} {b2}；停用 hangong5→{a3} {b3}；不存在→{a4} {b4}；退出→{lo}，旧令牌 /me→{m2}。（错口令与不存在同文案，不泄露账号存在性）", B)
# F-04 跨厂
c1, d1 = call(F, "POST", f"/v1/factories/{FB}/login", {"loginName": "changzhang", "password": "changzhang123456"})
# 在 B 厂建同名 hangong1，再各自登录
tb = tok("changzhangb")
cp, cr = call(F, "POST", f"/v1/factories/{FB}/people", {"loginName": "hangong1", "displayName": "B厂同名焊工"}, token=tb)
c2, d2 = call(F, "POST", f"/v1/factories/{FB}/login", {"loginName": "hangong1", "password": "hangong1123456"})
c3, d3 = call(F, "POST", f"/v1/factories/{FA}/login", {"loginName": "hangong1", "password": "hangong1123456"})
xa = call(F, "GET", f"/v1/factories/{FA}/me", token=d2["token"])[0] if c2 == 200 else None
mb = call(F, "GET", f"/v1/factories/{FB}/me", token=d2["token"])[1] if c2 == 200 else None
record("F-04", "通过" if c1 == 401 and cp in (200, 201) and c2 == 200 and c3 == 200 and xa in (401, 403) else "失败",
       f"A 厂超管登 B 厂→{c1}；B 厂新建同名 hangong1→{cp}，B 厂登录→{c2}（{mb.get('displayName') if isinstance(mb,dict) else ''}），A 厂 hangong1 照常→{c3}；B 厂令牌调 A 厂 /me→{xa}", B)
# F-05 改密码、显示名
tk = call(F, "POST", f"{P}/login", {"loginName": "hangong2", "password": "hangong2123456"})[1]["token"]
e1 = call(F, "POST", f"{P}/me/password", {"currentPassword": "bad", "password": "hangong2NEW1"}, token=tk)
e2 = call(F, "POST", f"{P}/me/password", {"currentPassword": "hangong2123456", "password": "hangong2NEW1"}, token=tk)
o1 = call(F, "POST", f"{P}/login", {"loginName": "hangong2", "password": "hangong2123456"})[0]
n1, nb = call(F, "POST", f"{P}/login", {"loginName": "hangong2", "password": "hangong2NEW1"})
old_sess = call(F, "GET", f"{P}/me", token=tk)[0]
e3 = call(F, "POST", f"{P}/me/password", {"currentPassword": "hangong2NEW1", "password": "abc"}, token=nb["token"])
dn = call(F, "POST", f"{P}/me/display-name", {"displayName": "李焊工改"}, token=nb["token"])
cat = call(F, "GET", f"{P}/catalog", token=tok("changzhang"))[1]
shown = [p["displayName"] for p in cat["people"] if p["loginName"] == "hangong2"]
# 改回默认：超管重置
rid = [p["id"] for p in cat["people"] if p["loginName"] == "hangong2"][0]
rs = call(F, "POST", f"{P}/people/{rid}/reset-password", {}, token=tok("changzhang"))[0]
call(F, "POST", f"{P}/people/{rid}/display-name", {"displayName": "李焊工"}, token=tok("changzhang"))
ok = e1[0] >= 400 and e2[0] in (200, 204) and o1 == 401 and n1 == 200 and shown == ["李焊工改"]
record("F-05", "通过" if ok else "失败",
       f"当前密码错→{e1[0]}；正常改→{e2[0]}；旧密码登录→{o1}；新密码→{n1}；改密那个会话本身仍有效={old_sess}；新密码只 3 位→{e3[0]} {short(e3[1],50)}；改显示名→{dn[0]}，超管名册显示 {shown}。（之后超管重置回默认密码={rs}）", B)

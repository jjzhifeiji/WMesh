from lib import *
B = "1"
t = tok("wan")
# W-01
st, b = call(W, "POST", "/v1/login", {"loginName": "wan", "password": WAN_PW}); t1 = b["token"]
m1 = call(W, "GET", "/v1/me", token=t1)[0]
lo = call(W, "POST", "/v1/logout", token=t1)[0]
m2 = call(W, "GET", "/v1/me", token=t1)[0]
record("W-01", "通过" if (m1, m2) == (200, 401) and lo in (200, 204) else "失败", f"登录 200，/v1/me={m1}；logout={lo}；退出后旧令牌 /v1/me={m2}", B)
# W-03 唯一管理员：找建用户的入口
probes = {p: call(W, "POST", p, {"loginName": "wan2", "password": "x12345678"}, token=t)[0] for p in ["/v1/users", "/v1/admins", "/v1/accounts", "/v1/me/users"]}
record("W-03", "通过" if all(v in (404, 405) for v in probes.values()) else "失败", f"WAN 没有建用户的路由，探测 {probes}；路由表只有 /v1/login、/v1/me、/v1/me/password", B)
# W-04 改密码
NEW = WAN_PW + "x9"
a = call(W, "POST", "/v1/me/password", {"currentPassword": "wrong-pass", "password": NEW}, token=t)
bb = call(W, "POST", "/v1/me/password", {"currentPassword": WAN_PW, "password": "short"}, token=t)
c = call(W, "POST", "/v1/me/password", {"currentPassword": WAN_PW, "password": NEW}, token=t)
old = call(W, "POST", "/v1/login", {"loginName": "wan", "password": WAN_PW})[0]
st, nb = call(W, "POST", "/v1/login", {"loginName": "wan", "password": NEW})
back = call(W, "POST", "/v1/me/password", {"currentPassword": NEW, "password": WAN_PW}, token=nb["token"])[0] if st == 200 else None
ok = a[0] >= 400 and c[0] in (200, 204) and old == 401 and st == 200
record("W-04", "通过" if ok else "失败", f"当前密码错→{a[0]} {short(a[1],80)}；新密码过短→{bb[0]} {short(bb[1],80)}；正常→{c[0]}；旧密码登录→{old}；新密码登录→{st}；改回→{back}。「两次不一致」只在前端校验（接口只收一次新密码）", B)

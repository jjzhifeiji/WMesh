from lib import *
import time
B = "1"
P = f"/v1/factories/{FA}"
ts = tok("changzhang")
C = lambda p, b=None, m="POST", t=None: call(F, m, P + p, b, token=t or ts)
cat = C("/catalog", m="GET")[1]
U = {u["name"]: u["id"] for u in cat["orgUnits"]}
PP = {p["loginName"]: p["id"] for p in cat["people"]}
# F-10 组织树
s1, a = C("/org-units", {"name": "F10甲", "parentId": None})
s2, b = C("/org-units", {"name": "F10乙", "parentId": a["id"]})
s3, c = C("/org-units", {"name": "F10丙", "parentId": b["id"]})
s4, _ = C(f"/org-units/{a['id']}/move", {"parentId": c["id"]})        # 成环
s5, _ = C(f"/org-units/{a['id']}/move", {"parentId": a["id"]})        # 挂自己
s6, _ = C(f"/org-units/{c['id']}/rename", {"name": "F10丙改"})
s7, _ = C(f"/org-units/{c['id']}/move", {"parentId": a["id"]})        # 改挂
s7b, r7b = C("/org-units", {"name": "F10乙", "parentId": a["id"]})    # 同父同名
units = {u["id"]: u for u in C("/catalog", m="GET")[1]["orgUnits"]}
record("F-10", "通过" if s4 >= 400 and s5 >= 400 and s6 == 200 and s7 == 200 and units[c["id"]]["parentId"] == a["id"] else "失败",
       f"建三层→{s1}/{s2}/{s3}；把根挂到孙下（成环）→{s4}；挂自己→{s5}；改名→{s6}；孙改挂到根→{s7}（parentId 变为根，单父字段，结构上不可能多父）；同父同名→{s7b} {short(r7b,50)}", B)
# F-11
d1, r1 = C(f"/org-units/{a['id']}/disable", {})                  # 有有效下级
d2, r2 = C(f"/org-units/{a['id']}", m="DELETE")                   # 有下级
d3, r3 = C(f"/org-units/{U['焊接一组']}", m="DELETE")            # 有人
d4, r4 = C(f"/org-units/{c['id']}/disable", {})                  # 叶子停用
d5, r5 = C(f"/org-units/{c['id']}", m="DELETE")                   # 叶子删除
record("F-11", "通过" if d1 >= 400 and d2 >= 400 and d3 >= 400 and d4 == 200 and d5 in (200, 204) else "失败",
       f"有有效下级时停用→{d1} {short(r1,50)}；有下级时删除→{d2} {short(r2,50)}；有人（焊接一组）时删除→{d3} {short(r3,50)}；叶子停用→{d4}；叶子删除→{d5}", B)
# F-12
n, acc = C("/people", {"loginName": "f12new", "displayName": "新人"})
l = call(F, "POST", P + "/login", {"loginName": "f12new", "password": "f12new123456"})
record("F-12", "通过" if n == 201 and l[0] == 200 else "失败", f"新建→{n}，回包不含密码={'password' not in json.dumps(acc)}；默认密码 f12new123456 登录→{l[0]}", B)
nid = acc["account"]["id"]
# F-13 授 / 收角色
g1, gr = C("/grants", {"personId": nid, "role": "org_admin"})
tk = call(F, "POST", P + "/login", {"loginName": "f12new", "password": "f12new123456"})[1]["token"]
before = C("/org-units", {"name": "F13管理员建", "parentId": None}, t=tk)
rv, _ = C(f"/grants/{gr['id']}/revoke", {})
after = C("/org-units", {"name": "F13收回后建", "parentId": None}, t=tk)
after2 = C("/assets", {"kind": "process", "level": "factory", "name": "F13收回后厂级", "content": "{}", "weldKind": "single"}, t=tk)
g2, gr2 = C("/grants", {"personId": nid, "role": "factory_super_admin"})
g3, gr3 = C("/grants", {"personId": nid, "role": "operator"})
gbad, rbad = C("/grants", {"personId": nid, "role": "auditor"})
record("F-13", "通过" if g1 in (200, 201) and rv == 200 and after[0] == 403 and g2 in (200, 201) and g3 in (200, 201) and gbad >= 400 else "失败",
       f"授管理员→{g1}；（注：组织管理属超管）以管理员身份建组织→{before[0]}；收回→{rv}；原会话再建组织→{after[0]}、建厂级工艺→{after2[0]} {short(after2[1],40)}；授超管→{g2}；授操作员→{g3}；授 auditor→{gbad} {short(rbad,40)}（⚠口径K1 只有三种角色）", B)
# F-14 最后一个超管
sa = PP["changzhang"]
grants = C("/catalog", m="GET")[1]["roleGrants"]
# 先收回刚授给 f12new 的超管，让 changzhang 成唯一超管
for g in grants:
    if g["personId"] == nid and g["role"] == "factory_super_admin" and g["status"] == "active":
        C(f"/grants/{g['id']}/revoke", {})
grants = C("/catalog", m="GET")[1]["roleGrants"]
sag = [g for g in grants if g["personId"] == sa and g["role"] == "factory_super_admin" and g["status"] == "active"][0]
x1, y1 = C(f"/grants/{sag['id']}/revoke", {})
x2, y2 = C(f"/people/{sa}/disable", {})
record("F-14", "通过" if x1 >= 400 and x2 >= 400 else "失败", f"唯一超管 changzhang：收回超管→{x1} {short(y1,50)}；停用→{x2} {short(y2,50)}", B)
# F-16 所属组织
o1 = C(f"/people/{nid}/org", {"orgUnitId": U["焊接二组"]})[0]
o2 = C(f"/people/{nid}/org", {"orgUnitId": c["id"]})[0]            # 已删除的节点
o3 = C(f"/people/{nid}/org", {"orgUnitId": None})[0]
record("F-16", "通过" if o1 == 200 and o2 >= 400 and o3 == 200 else "失败",
       f"设到焊接二组→{o1}；设到已删除节点→{o2}；设为厂直属→{o3}。焊次组织归属按 ⚠口径K3（按人当前组织）在批 3 I-10 验", B)
json.dump({"nid": nid}, open(".b1_f12.json", "w"))

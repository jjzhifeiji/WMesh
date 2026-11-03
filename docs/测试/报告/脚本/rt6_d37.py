"""D-37 复测：经厂端/WAN 接口建 T 排、单层工程，读回 /content 看字段是否保留、未采点不补 0。"""
from lib import *
import json, uuid, copy, sys
P = f"/v1/factories/{FA}"
def pose(x, y, z): return {"x": x, "y": y, "z": z, "rx": -163.355, "ry": 3.986, "rz": 111.582}
J = [25.773, -124.37, 90.55, -51.962, -85.235, 4.75]
def pt(t, p=None):
    d = {"id": str(uuid.uuid4()), "type": t}
    if p: d.update(pose=p, jointAngles=J, scanPose=p, scanJoints=J)
    return d
def tbar(name):
    return [{
        "id": str(uuid.uuid4()), "name": "焊道 1", "kind": "tbar", "templateId": "44444444-4444-4444-8444-444444444444", "isEnabled": True, "selectedPointIndex": 0,
        "teachMode": "LASER", "plateThickness": 12.5, "grooveAngleDeg": 35.0, "extendUpMm": 20.0, "extendDownMm": 10.0,
        "laserWeldLengthMm": 150.0, "laserLowerWidthMm": 1.2, "laserUpperWidthMm": 1.4,
        "gapBands": [{"minGap": 0.0, "maxGap": 5.0, "layer": 1}, {"minGap": 5.0, "maxGap": 10.0, "layer": 1}],
        "points": [pt("START_SAFE", pose(-845.86, -522.0, -105.4)), pt("GROOVE_A_LOWER", pose(-845.86, -472.0, -105.4)),
                   pt("GROOVE_B_LOWER"), pt("GROOVE_A_UPPER"), pt("GROOVE_B_UPPER"),
                   pt("LASER_BASE_1"), pt("LASER_BASE_2"), pt("START"), pt("END"), pt("END_SAFE")],
    }]
def single(_=None):
    pa, pb = str(uuid.uuid4()), str(uuid.uuid4())
    def path(i, pid):
        return {"id": pid, "name": f"焊道{i}", "kind": "single", "templateId": "11111111-1111-4111-8111-111111111111", "isEnabled": True, "selectedPointIndex": 0, "processId": "",
                "extendLengthMm": 15.0 + i,
                "points": [pt("START_SAFE", pose(-845, -522, -105)), pt("START", pose(-845, -472, -105)), pt("END", pose(-845, -472, 95)), pt("END_SAFE", pose(-845, -522, 95))]}
    corner = path(3, str(uuid.uuid4())); corner["name"] = "包角焊道 3 (层 1)"
    corner["cornerGroupParams"] = {"groupId": str(uuid.uuid4()), "refPathAId": pa, "refPathBId": pb, "layerCount": 1, "initialLength": 8.0,
                                   "upwardOffset": 1.0, "lengthReduction": 0.0, "isMaster": True, "torchRx": 175.39, "torchRy": 4.39, "torchRz": 110.67,
                                   "processId": "", "passes": [{"length": 8.0, "flatOffset": 8.0, "upwardOffset": 1.0}]}
    return [path(1, pa), path(2, pb), corner]
KEYS = ["teachMode", "plateThickness", "grooveAngleDeg", "extendUpMm", "extendDownMm", "laserWeldLengthMm", "laserLowerWidthMm", "laserUpperWidthMm", "gapBands"]
def check_tbar(src, got):
    bad = []
    s, g = src[0], got[0]
    for k in KEYS:
        gv = [{kk: b.get(kk) for kk in ("minGap", "maxGap", "layer")} for b in g.get(k) or []] if k == "gapBands" else g.get(k)
        if gv != s.get(k): bad.append(f"{k}: 送 {s.get(k)} 回 {g.get(k)}")
    for sp, gp in zip(s["points"], g["points"]):
        if "pose" not in sp:
            extra = [k for k in ("pose", "refPointX", "jointAngles", "scanPose", "scanJoints") if k in gp]
            if extra: bad.append(f"未采点 {gp['type']} 多出 {extra}={short(gp,160)}")
        elif gp.get("pose", {}).get("x") != sp["pose"]["x"]: bad.append(f"{gp['type']} 位姿变了")
    return bad
def check_single(src, got):
    bad = []
    for sp, gp in zip(src, got):
        if gp.get("extendLengthMm") != sp.get("extendLengthMm"): bad.append(f"{sp['name']} extendLengthMm 送 {sp.get('extendLengthMm')} 回 {gp.get('extendLengthMm')}")
        if "cornerGroupParams" in sp:
            a, b = sp["cornerGroupParams"], gp.get("cornerGroupParams") or {}
            for k, v in a.items():
                if b.get(k) != v: bad.append(f"cornerGroupParams.{k} 送 {v} 回 {b.get(k)}")
    if len(got) != len(src): bad.append(f"条数 {len(src)}→{len(got)}")
    return bad
out = {}
# 厂端
t = tok("gongyi1")
for label, mk, wk, chk in (("厂T排", tbar, "tbar", check_tbar), ("厂单层", single, "single", check_single)):
    c = mk(label)
    s, a = call(F, "POST", P + "/assets", {"kind": "project", "level": "factory", "name": "RT6-" + label, "content": json.dumps(c, ensure_ascii=False), "weldKind": wk}, token=t)
    print(label, "create", s, short(a, 200))
    if s != 201: out[label] = f"建失败 {s} {short(a)}"; continue
    s2, r = call(F, "POST", P + f"/assets/{a['id']}/publish", {"expected": a["revision"]}, token=t); print(" publish", s2, short(r, 120))
    s3, b = call(F, "GET", P + f"/assets/{a['id']}/content", token=t)
    got = json.loads(b["content"]) if isinstance(b, dict) and "content" in b else b
    bad = chk(c, got)
    out[label] = {"id": a["id"], "bad": bad, "sample": short(got[0] if label.endswith("T排") else got[2], 900)}
    print(" bad:", bad)
# WAN
tw = tok("wan")
for label, mk, wk, chk in (("WAN T排", tbar, "tbar", check_tbar), ("WAN单层", single, "single", check_single)):
    c = mk(label)
    s, a = call(W, "POST", "/v1/assets", {"kind": "project", "name": "RT6-" + label, "content": json.dumps(c, ensure_ascii=False), "weldKind": wk}, token=tw)
    print(label, "create", s, short(a, 200))
    if s != 201: out[label] = f"建失败 {s} {short(a)}"; continue
    s3, b = call(W, "GET", f"/v1/assets/{a['id']}/content", token=tw)
    got = json.loads(b["content"]) if isinstance(b, dict) and "content" in b else b
    bad = chk(c, got)
    out[label] = {"id": a["id"], "bad": bad, "sample": short(got[0] if "T排" in label else got[2], 900)}
    print(" bad:", bad)
json.dump(out, open(".rt6_d37.json", "w"), ensure_ascii=False, indent=1)

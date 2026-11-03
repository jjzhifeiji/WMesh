from lib import *
import paho.mqtt.client as mqtt, time
B = "3"
cl = call(W, "GET", "/v1/clients", token=tok("wan"))[1]
c2 = [c for c in cl if c["shortCode"] == "C0002"][0]["id"]; c1 = [c for c in cl if c["shortCode"] == "C0001"][0]["id"]
c3 = [c for c in cl if c["shortCode"] == "C0003"][0]["id"]
pt = pad_login("hangong3", clientId=c2)
res = {}
def run(label, user, pw, cid, subs):
    out = {"granted": {}, "conn": None, "msgs": []}
    m = mqtt.Client(client_id=cid, clean_session=True)
    m.username_pw_set(user, pw)
    def on_connect(c, u, f, rc): out["conn"] = rc
    def on_sub(c, u, mid, granted): out["granted"][mid] = granted
    m.on_connect = on_connect; m.on_subscribe = on_sub
    m.on_message = lambda c, u, msg: out["msgs"].append(msg.topic)
    try:
        m.connect("127.0.0.1", 52184, 10); m.loop_start(); time.sleep(1.5)
        mids = {}
        for t in subs:
            r, mid = m.subscribe(t, 1); mids[mid] = t
        time.sleep(1.5)
        out["granted"] = {mids.get(k, k): v for k, v in out["granted"].items()}
        m.loop_stop(); m.disconnect()
    except Exception as e:
        out["err"] = str(e)
    res[label] = out
run("本机 C0002 + 自己 topic / 别台 C0001 topic / B 厂 C0003 topic / 通配", FA, pt, c2,
    [f"factory/{FA}/client/{c2}/down", f"factory/{FA}/client/{c1}/down", f"factory/{FB}/client/{c3}/down", f"factory/{FA}/client/+/down", "#"])
run("错口令", FA, "bad-token", c2, [])
run("A 厂令牌冒充 B 厂", FB, pt, c3, [f"factory/{FB}/client/{c3}/down"])
print(json.dumps(res, ensure_ascii=False, indent=1))
own = res[list(res)[0]]["granted"]
ok = list(own.values())[0][0] in (0, 1) and all(v[0] == 128 for k, v in list(own.items())[1:]) and res["错口令"]["conn"] not in (0, None) and res["A 厂令牌冒充 B 厂"]["conn"] not in (0, None)
record("S-06", "通过" if ok else "失败", f"paho 直连厂端 MQTT 52184：{json.dumps({k: (v['conn'], v['granted']) for k, v in res.items()}, ensure_ascii=False)}（connect rc 0=成功；订阅授权 0/1=允许，128=拒绝）", B)

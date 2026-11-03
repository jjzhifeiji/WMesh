from lib import *
import paho.mqtt.client as mqtt, time, sys
cl = call(W, "GET", "/v1/clients", token=tok("wan"))[1]
c2 = [c for c in cl if c["shortCode"] == "C0002"][0]["id"]
pt = pad_login("hangong3", clientId=c2)
msgs = []
m = mqtt.Client(client_id=c2, clean_session=True); m.username_pw_set(FA, pt)
m.on_message = lambda c, u, msg: msgs.append((msg.topic, msg.payload.decode("utf-8", "replace")))
m.connect("127.0.0.1", 52184, 10); m.subscribe(f"factory/{FA}/client/{c2}/down", 1); m.loop_start(); time.sleep(1)
ts = tok("changzhang"); P = f"/v1/factories/{FA}"
pol = call(F, "GET", P + "/client-policy", token=ts)[1]
call(F, "PUT", P + "/client-policy", {"maxCachedProjects": 2, "cacheScope": "all", "persistUnwrapKey": pol["persistUnwrapKey"], "keyTtlSeconds": pol["keyTtlSeconds"], "encryptPouch": True}, token=ts)
time.sleep(4); m.loop_stop(); m.disconnect()
json.dump(msgs, open(".mqtt_cap.json", "w"), ensure_ascii=False)
print(msgs)

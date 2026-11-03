from b4 import *
from lib import *
import re
DUR = 30 * 60
t1 = psql("select now()").strip()
n_start(project="第七轴-单道")
t0 = time.time(); runs = 0; done = 0; inj = []; mem = []; nxt = 600
def pss():
    o = sh("dumpsys meminfo com.gbndt.shijiaoqi | grep -E 'TOTAL PSS|TOTAL:'")
    m = re.search(r"TOTAL(?: PSS)?:?\s+(\d+)", o); return int(m.group(1)) if m else None
mem.append((0, pss()))
while time.time() - t0 < DUR:
    m = logmark(); runs += 1; arc_run(speed=5, timeout=60)
    if "程序跑完" in logsince(m): done += 1
    el = time.time() - t0
    if el > nxt:
        k = len(inj) % 2
        if k == 0: fake("mute"); time.sleep(3); fake("unmute")
        else: fake("link off"); time.sleep(5); fake("link on")
        inj.append((round(el), ["mute", "link"][k])); time.sleep(8); nxt += 600
        mem.append((round(el), pss()))
mem.append((round(time.time() - t0), pss()))
alive = "shijiaoqi" in sh("pidof com.gbndt.shijiaoqi") or bool(sh("pidof com.gbndt.shijiaoqi").strip())
conns = sh("cat /proc/net/tcp6 | grep -ic ':1F90 '")
home(); time.sleep(20)
n = psql(f"select count(*) from weld_facts where created_at > '{t1}'")
json.dump({"runs": runs, "done": done, "inj": inj, "mem": mem, "facts": n, "alive": alive, "conns8080": conns}, open(".e30.json", "w"), ensure_ascii=False)
print("E30DONE", runs, done, n)

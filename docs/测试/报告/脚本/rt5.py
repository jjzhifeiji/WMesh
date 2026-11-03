from b4 import *
from lib import *
import json, subprocess
def padlog(pat=None, n=20):
    out = adb("shell", "run-as com.gbndt.shijiaoqi sh -c 'cat files/logs/*.ndjson'", timeout=60)
    ls = [l for l in out.splitlines() if l.startswith('{') and (not pat or re.search(pat, l))]
    def k(l):
        m = re.search(r'"ts":"(\d+)"', l); return int(m.group(1)) if m else 0
    ls.sort(key=k)
    return [re.sub(r'^\{"ts":"\d+","level":"\w+","line":"', '', l)[:400] for l in ls[-n:]]
RT = os.path.join(HERE, "retest_blocked.json")
def rec(case, result, evidence):
    d = json.load(open(RT, encoding="utf-8")) if os.path.exists(RT) else {}
    d[case] = {"result": result, "evidence": evidence}
    json.dump(d, open(RT, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(case, result)
def kb_shown():
    return "mInputShown=true" in sh("dumpsys input_method | grep mInputShown")
def hide_kb():
    if kb_shown(): back(); time.sleep(0.6)
def set_field(n, val):
    tap(n['x'], n['y']); time.sleep(0.8)
    sh('input keyevent KEYCODE_MOVE_END ' + ' '.join(['67']*12)); 
    for ch in val: sh(f"input text '{ch}'"); time.sleep(0.15)
    time.sleep(0.5); hide_kb()
def cap(label, dx=0, dy=0, dz=0):
    if dx or dy or dz:
        print(fake(f"move {dx} {dy} {dz}")); time.sleep(1.0)
    tapt(label, exact=True); time.sleep(0.6)
    m = len(padlog(None, 100000))
    tapt("采集数据", exact=True); time.sleep(1.2)
    return padlog("Toast", 2)
KP={'1':(70,517),'2':(202,517),'3':(330,517),'4':(70,648),'5':(202,648),'6':(330,648),'7':(70,775),'8':(202,775),'9':(330,775),'0':(202,905),'.':(330,905),'-':(460,648)}
def ime_type(n, s, clear=12):
    """点字段→全屏讯飞数字键盘；退格清空后逐键点，最后按回退收起。"""
    tap(n['x'], n['y']); time.sleep(1.0)
    for _ in range(clear): sh('input tap 1126 512')
    for ch in s: sh(f'input tap {KP[ch][0]} {KP[ch][1]}')
    time.sleep(0.5); back(); time.sleep(0.8)
def pick(name):
    for _ in range(8):
        n = find(name, exact=True)
        if n and 150 < n['y'] < 960: tap(n['x'], n['y']); time.sleep(1.5); return True
        swipe(600, 800, 600, 400, 400); time.sleep(0.8)
    raise RuntimeError('no ' + name)
def run_hold(label, timeout=40):
    m = logmark()
    n = find(label); hold(n['x'], n['y'], 1600)
    for _ in range(timeout):
        if '程序跑完' in logsince(m): break
        time.sleep(1)
    time.sleep(3)
    s = logsince(m)
    luas = re.findall(r'存到 (\S+\.lua)', s)
    return s, luas

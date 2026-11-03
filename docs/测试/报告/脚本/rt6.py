from rt5 import *
RT6 = os.path.join(HERE, "retest_d36_40.json")
def rec6(case, result, evidence):
    d = json.load(open(RT6, encoding="utf-8")) if os.path.exists(RT6) else {}
    d[case] = {"result": result, "evidence": evidence}
    json.dump(d, open(RT6, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(case, result)
def rbtn(label, tries=6):
    """右栏找按钮，滚到可见再点。"""
    for _ in range(tries):
        n = find(label, exact=True)
        if n and 120 < n['y'] < 1000:
            tap(n['x'], n['y']); return n
        swipe(950, 850, 950, 450, 400) if (not n or n['y'] >= 1000 or n['y'] == 0) else swipe(950, 450, 950, 850, 400)
        time.sleep(0.6)
    raise RuntimeError('no btn ' + label)
def collect(label=None, wait=1.5):
    if label: tapt(label, exact=True); time.sleep(0.6)
    rbtn('采集数据'); time.sleep(wait)
    return padlog('Toast', 2)
def tap_list(name):
    for _ in range(5):
        ns = [n for n in nodes() if n['text'] == name and n['y'] > 200 and n['x'] > 600]
        if ns and ns[0]['y'] < 1000:
            tap(ns[0]['x'], ns[0]['y']); time.sleep(4); return True
        swipe(974, 950, 974, 350, 400); time.sleep(1)
    raise RuntimeError('no ' + name)

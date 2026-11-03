from pad import *
import time, sys
def enter_single():
    if not find("模拟焊接") and not find("暂停"):
        tapt("单层单道"); time.sleep(3)
def ui_buttons():
    return [n["text"] for n in nodes() if n["text"] in ("模拟焊接","起弧焊接","暂停","继续","长按继续","停止","微调")]

def pad_login_ui(name, pwd, remember=None, factory="测试厂A · 192.168.123.150", wait=6):
    """在登录页选厂、填名密码并点登录。名只支持小写字母+数字（数字结尾单独补发）。"""
    for _ in range(20):
        if not find("扫描中"): break
        time.sleep(1)
    n = find("厂服务")
    sel = [x for x in nodes() if "·" in (x["text"] or "") and x["y"] < 520]
    if not sel or factory not in sel[0]["text"]:
        tap(sel[0]["x"] if sel else 490, sel[0]["y"] if sel else 474); time.sleep(1)
        tapt(factory)
        time.sleep(0.5)
    ns = nodes()
    fields = [x for x in ns if x["click"] and 600 < x["y"] < 700 and x["x"] < 1000]
    name_f, pwd_f = sorted(fields, key=lambda x: x["x"])[:2]
    tap(name_f["x"], name_f["y"]); sh("input keyevent KEYCODE_MOVE_END " + " ".join(["67"] * 20))
    letters = name.rstrip("0123456789"); digits = name[len(letters):]
    kbtype(letters, done=True)
    if digits:
        tap(name_f["x"], name_f["y"]); sh("input keyevent KEYCODE_MOVE_END"); sh(f"input text {digits}")
        time.sleep(0.8); tap(1083, 175); time.sleep(0.5)
    if find("执行") or find("完成"):
        back()
    tap(pwd_f["x"], pwd_f["y"]); sh("input keyevent KEYCODE_MOVE_END " + " ".join(["67"] * 20))
    text(pwd, done=True)
    if remember is not None:
        cb = [x for x in nodes() if x["click"] and abs(x["y"] - 791) < 30 and x["x"] < 700]
        chk = [x for x in nodes() if x["text"] == "记住账号密码"]
        # 复选框状态读 checked
        box = cb[0] if cb else None
        if box and box["checked"] != remember:
            tap(box["x"], box["y"])
    tapt("登录", exact=True, wait=wait)

def pad_logout():
    for _ in range(4):
        if find("焊接作业模式选择"): break
        back(); time.sleep(1.5)
    tapt(find("焊接作业模式选择") and [n for n in nodes() if n["x"]>1000 and n["y"]<150 and n["text"]][0]["text"], exact=True); time.sleep(2)
    tap(883, 1000); time.sleep(3)

def arc_run(speed=10, timeout=60):
    fake(f"speed {speed}"); m = logmark(); hold(1068, 344, 1600)
    for _ in range(timeout):
        if '程序跑完' in logsince(m): break
        time.sleep(1)
    time.sleep(2)

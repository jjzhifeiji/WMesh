"""adb 驱动示教器：点、输、截图、读 UI 树、假控制器注入。"""
from __future__ import annotations
import os, re, subprocess, sys, time, xml.etree.ElementTree as ET
HERE = os.path.dirname(os.path.abspath(__file__))
SHOT = os.path.join(HERE, "..", "截图")
SER = "YARMA11B0V21D22106170304"
PKG = "com.gbndt.shijiaoqi"
FAKE_LOG = os.path.join(HERE, "../../../../client/ShiJiaoQi-New/scripts/fake_robot_runs/fake.log")


def adb(*a, check=False, timeout=60):
    r = subprocess.run(["adb", "-s", SER, *a], capture_output=True, text=True, timeout=timeout)
    return r.stdout + r.stderr


def sh(cmd):
    return adb("shell", cmd)


def tap(x, y, wait=0.6):
    sh(f"input tap {int(x)} {int(y)}"); time.sleep(wait)


def hold(x, y, ms=1500, wait=0.5):
    sh(f"input swipe {int(x)} {int(y)} {int(x)} {int(y)} {ms}"); time.sleep(wait)


def swipe(x1, y1, x2, y2, ms=300):
    sh(f"input swipe {x1} {y1} {x2} {y2} {ms}"); time.sleep(0.5)


def key(k):
    sh(f"input keyevent {k}"); time.sleep(0.4)


def back():
    key(4)


def text(s, done=False):
    """逐字符输入（输入法会打乱整串）；ASCII 才可靠。"""
    for ch in s:
        c = ch.replace("\\", "\\\\").replace("'", "\\'").replace(" ", "%s").replace("&", "\\&").replace("(", "\\(").replace(")", "\\)").replace(";", "\;").replace("<", "\\<").replace(">", "\\>").replace("|", "\\|").replace("*", "\\*").replace("$", "\\$")
        sh(f"input text '{c}'" if ch not in "'" else 'input text "\'"')
        time.sleep(0.12)
    time.sleep(1.0)
    if done:
        tap(1083, 175)


def nodes():
    sh("uiautomator dump /sdcard/ui.xml >/dev/null 2>&1")
    xml = adb("exec-out", "cat", "/sdcard/ui.xml")
    try:
        root = ET.fromstring(xml[xml.find("<?xml"):] if "<?xml" in xml else xml)
    except Exception:
        return []
    out = []
    for n in root.iter("node"):
        b = re.findall(r"\d+", n.get("bounds", "[0,0][0,0]"))
        x1, y1, x2, y2 = map(int, b)
        out.append({"text": n.get("text", ""), "desc": n.get("content-desc", ""), "rid": n.get("resource-id", ""),
                    "cls": n.get("class", ""), "click": n.get("clickable") == "true", "enabled": n.get("enabled") == "true",
                    "checked": n.get("checked") == "true", "focused": n.get("focused") == "true",
                    "x": (x1 + x2) // 2, "y": (y1 + y2) // 2, "b": (x1, y1, x2, y2)})
    return out


def texts():
    return [n["text"] or n["desc"] for n in nodes() if n["text"] or n["desc"]]


def find(s, exact=False, ns=None):
    for n in ns or nodes():
        t = n["text"] or n["desc"]
        if (t == s) if exact else (s in t):
            return n
    return None


def tapt(s, exact=False, wait=0.8, ns=None):
    n = find(s, exact, ns)
    if not n:
        raise RuntimeError(f"屏上没有「{s}」：{texts()[:40]}")
    tap(n["x"], n["y"], wait)
    return n


def wait_for(s, timeout=20, exact=False):
    end = time.time() + timeout
    while time.time() < end:
        n = find(s, exact)
        if n:
            return n
        time.sleep(1)
    return None


def shot(name):
    os.makedirs(SHOT, exist_ok=True)
    p = os.path.join(SHOT, name + ".png")
    with open(p, "wb") as f:
        f.write(subprocess.run(["adb", "-s", SER, "exec-out", "screencap", "-p"], capture_output=True).stdout)
    return os.path.relpath(p, os.path.join(HERE, ".."))


def fake(cmd):
    r = subprocess.run(f"echo '{cmd}' | nc -w 2 127.0.0.1 8090", shell=True, capture_output=True, text=True)
    return r.stdout.strip()


def logmark():
    return os.path.getsize(FAKE_LOG)


def logsince(mark):
    with open(FAKE_LOG, encoding="utf-8", errors="replace") as f:
        f.seek(mark)
        return f.read()


def restart_app():
    sh(f"am force-stop {PKG}"); time.sleep(1)
    sh(f"monkey -p {PKG} -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1"); time.sleep(4)


def logcat_clear():
    adb("logcat", "-c")


def logcat(grep=None):
    out = adb("logcat", "-d", "-v", "time", timeout=30)
    return "\n".join(l for l in out.splitlines() if not grep or re.search(grep, l))


ROOTP = os.path.abspath(os.path.join(HERE, "../../../.."))


def restart_fake(serial="FA:KE:00:00:00:01", extra="", kill_only=False):
    """重启假控制器（日志追加到 fake.log，不截断）。"""
    subprocess.run("pkill -f scripts/fake_robot.py", shell=True)
    time.sleep(1)
    if kill_only:
        return
    start = os.path.join(ROOTP, "docs/device-date/projects/single_layer/" + "第七轴/平焊/project_data.json")
    import json as _j
    seed = _j.load(open(os.path.join(ROOTP, "scripts/testenv/seed.json"), encoding="utf-8"))
    start = os.path.join(ROOTP, "docs/device-date/projects", seed["fake_robot"]["start_pose_from"])
    cmd = f'nohup python3 "{ROOTP}/client/ShiJiaoQi-New/scripts/fake_robot.py" --serial "{serial}" --laser-host 0.0.0.0 --start-from "{start}" {extra} >>"{FAKE_LOG}" 2>&1 &'
    subprocess.run(cmd, shell=True)
    time.sleep(2)


def keytext(s, done=True, clear=0):
    """用按键事件逐个输入 a-z0-9（绕开讯飞输入法乱改大小写、吞数字）。"""
    codes = []
    for ch in s:
        if ch.isdigit():
            codes.append(f"KEYCODE_{ch}")
        elif ch.isalpha():
            codes.append(f"KEYCODE_{ch.upper()}")
        elif ch == "-":
            codes.append("KEYCODE_MINUS")
        elif ch == ".":
            codes.append("KEYCODE_PERIOD")
        elif ch == "_":
            codes.append("--longpress KEYCODE_MINUS")
    if clear:
        sh("input keyevent KEYCODE_MOVE_END " + " ".join(["67"] * clear))
    sh("input keyevent " + " ".join(codes))
    time.sleep(0.8)
    if done:
        tap(1083, 175)


_KB = {}
for _row, _y, _xs in (("qwertyuiop", 520, [66, 186, 304, 422, 540, 658, 776, 895, 1013, 1131]),
                      ("asdfghjkl", 650, [126, 246, 364, 482, 600, 718, 836, 955, 1073]),
                      ("zxcvbnm", 780, [244, 363, 481, 599, 718, 836, 955])):
    for _c, _x in zip(_row, _xs):
        _KB[_c] = (_x, _y)


def kbtype(s, done=True, clear=0):
    """在讯飞全屏输入框里点软键盘输入小写字母（输入法吞数字、乱大小写时用）。"""
    for _ in range(clear):
        sh("input tap 1106 772")
    for ch in s:
        x, y = _KB[ch]
        sh(f"input tap {x} {y}")
    time.sleep(0.5)
    if done:
        tap(1083, 175)

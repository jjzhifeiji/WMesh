from b2_run import *
import subprocess
SIM = "/Users/jerry/WY/WMesh/client/ShiJiaoQi-New/scripts/sim_net.sh"


def net(mode):
    r = subprocess.run([SIM, mode], capture_output=True, text=True)
    return (r.stdout + r.stderr).strip()


def n_start(who="hangong1", pwd=None, project=None):
    net("both")
    sh(f"pm clear {PKG}"); time.sleep(1)
    sh(f"monkey -p {PKG} -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1"); time.sleep(5)
    pad_login_ui(who, pwd or who + "123456", wait=6)
    for _ in range(40):
        if not find("正在同步厂服"): break
        time.sleep(2)
    if project:
        open_project(project)


def open_project(name, level="厂级", mode="单层单道"):
    home()
    tapt(mode); time.sleep(3); tapt("工程管理"); time.sleep(2); tapt(level if level != "个人级" else "个人级"); time.sleep(2)
    for _ in range(4):
        n = find(name)
        if n and n["y"] > 0: break
        swipe(974, 950, 974, 250, 400); time.sleep(1)
    tap(n["x"], n["y"]); time.sleep(4)


def home():
    for _ in range(5):
        if find("焊接作业模式选择"): return True
        if find("应用列表"):
            sh(f"monkey -p {PKG} -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1"); time.sleep(5); continue
        if find("正在同步厂服"):
            time.sleep(3); continue
        back(); time.sleep(1.5)
    return bool(find("焊接作业模式选择"))


def n_end():
    return net("both")

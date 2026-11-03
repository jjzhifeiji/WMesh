"""把 results.json 回填到 执行记录.md 的用例表。"""
import json, os, re
HERE = os.path.dirname(os.path.abspath(__file__))
REC = os.path.join(HERE, "..", "执行记录.md")
res = json.load(open(os.path.join(HERE, "results.json"), encoding="utf-8"))
out = []
for line in open(REC, encoding="utf-8").read().splitlines():
    m = re.match(r"^\| ([A-Z]-\d+) \|", line)
    if m and m.group(1) in res:
        cells = line.strip().strip("|").split("|")
        r = res[m.group(1)]
        cells[5] = f" {r['result']} "
        ev = r["evidence"].replace("|", "／").replace("\n", " ")
        cells[6] = f" {ev} "
        line = "|" + "|".join(cells) + "|"
    out.append(line)
open(REC, "w", encoding="utf-8").write("\n".join(out) + "\n")
from collections import Counter
print(Counter(v["result"] for v in res.values()), len(res))

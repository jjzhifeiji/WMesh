from lib import *
import json
ids=json.load(open('.rt5_tbar_ids.json'))
st,b=call(F,'GET',f'/v1/factories/{FA}/assets/01a19fe2-3854-77e0-93d2-28b9a5266184/content',token=tok('hangong1'))
c=json.loads(b['content'])
print([ (p['name'],p.get('teachMode'),p.get('plateThickness'),p.get('laserWeldLengthMm')) for p in c])
c[0]['gapBands']=[{"minGap":5.0,"maxGap":10.0,"layer":1},{"minGap":0.0,"maxGap":5.0,"layer":1}]
c[1]['gapBands']=[{"minGap":0.0,"maxGap":5.0,"layer":1,"rootProcessId":ids["RT5打底A"],"capProcessId":ids["RT5盖面A1"],"capProcessIds":[ids["RT5盖面A1"]]}]
t=tok('gongyi1')
s,a=call(F,"POST",f"/v1/factories/{FA}/assets",{"kind":"project","level":"factory","name":"RT5T排","content":json.dumps(c,ensure_ascii=False),"weldKind":"tbar"},token=t)
print(s,short(a,400))
s2,r=call(F,"POST",f"/v1/factories/{FA}/assets/{a['id']}/publish",{"expected":a["revision"]},token=t); print(s2)
open('.rt5_proj_id','w').write(a['id'])

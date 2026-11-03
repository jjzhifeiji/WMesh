from lib import *
import json
t=tok('gongyi1')
P=f"/v1/factories/{FA}"
base=json.load(open('.rt5_proc.json'))
specs=[("RT5打底A",151,21.1,4.0),("RT5盖面A1",201,22.1,5.0),("RT5盖面A2",211,23.1,6.0),
       ("RT5打底B",161,21.6,4.5),("RT5盖面B1",221,24.1,5.5),("RT5盖面B2",231,25.1,6.5)]
ids={}
for name,cur,vol,spd in specs:
    c=dict(base); c.update(name=name,current=cur,voltage=vol,speed=spd)
    s,a=call(F,"POST",P+"/assets",{"kind":"process","level":"factory","name":name,"content":json.dumps(c,ensure_ascii=False),"weldKind":"tbar"},token=t)
    assert s==201,(s,a)
    s2,r=call(F,"POST",P+f"/assets/{a['id']}/publish",{"expected":a["revision"]},token=t)
    print(name,s,s2,a['id'])
    ids[name]=a['id']
json.dump(ids,open('.rt5_tbar_ids.json','w'),ensure_ascii=False)

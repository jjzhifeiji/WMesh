from lib import *
import json
t=tok('wan')
out={}
for name,cp in (("RT5包角加密",False),("RT5包角公开",True)):
    c={"kind":"cornerGeometry","name":name,"passes":[{"length":8,"flatOffset":8,"upwardOffset":1}]}
    s,a=call(W,"POST","/v1/assets",{"kind":"process","name":name,"content":json.dumps(c,ensure_ascii=False),"weldKind":"single","copyable":cp},token=t)
    print(s,short(a))
    cur=call(W,"GET",f"/v1/assets/{a['id']}",token=t)[1]
    s2,r=call(W,"POST",f"/v1/assets/{a['id']}/publish",{"expected":cur["revision"]},token=t)
    print("pub",s2,short(r))
    out[name]=a['id']
print(out)

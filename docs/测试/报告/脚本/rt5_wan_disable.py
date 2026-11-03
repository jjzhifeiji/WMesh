from lib import *
import sys
t=tok('wan'); act=sys.argv[1]
al=call(W,'GET','/v1/assets',token=t)[1]
n=0
for a in al:
    if a['kind']=='process' and not a.get('corner') and not a['name'].startswith('RT5'):
        if (act=='disable' and a['status']=='available') or (act=='enable' and a['status']=='disabled'):
            s,r=call(W,'POST',f"/v1/assets/{a['id']}/{act}",{"expected":a["revision"]},token=t)
            n+=1
            if s!=200: print(a['name'],s,short(r,100))
print(act,n)

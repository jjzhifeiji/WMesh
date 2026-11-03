from lib import *
import sys
t=tok('gongyi1'); P=f"/v1/factories/{FA}"
al=call(F,'GET',P+'/assets',token=t)[1]
act=sys.argv[1]
for a in al:
    if a['kind']=='process' and a['level']=='factory' and not a['name'].startswith('RT5') and not a.get('corner'):
        if (act=='disable' and a['status']=='available') or (act=='enable' and a['status']=='disabled'):
            s,r=call(F,'POST',P+f"/assets/{a['id']}/{act}",{"expected":a["revision"]},token=t)
            print(a['name'],s)

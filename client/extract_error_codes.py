import fitz
import re

pdf_path = r"G:\法奥指令集\协作机器人控制器通讯指令协议用户手册.pdf"
doc = fitz.open(pdf_path)

for page_num in range(276, len(doc)):
    page = doc[page_num]
    text = page.get_text()
    print(text)

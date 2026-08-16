import re

p = r'e:\Trae-Work-Projects\lunitide\web\src\session\SessionPage.tsx'
t = open(p, encoding='utf-8').read()
line = t.split('\n')[29] if len(t.split('\n')) > 29 else ''
# 直接打印 useState 链式声明行片段
m = re.search(r'\[busy,setBusy\][^\n]{0,700}', t)
print(m.group(0)[:700] if m else 'busy decl not found')
print()
m = re.search(r'\[chatStatus,setChatStatus\][^\n]{0,200}', t)
print(m.group(0)[:200] if m else 'chatStatus decl not found')
print()
m = re.search(r'\[assistantText,setAssistantText\][^\n]{0,200}', t)
print(m.group(0)[:200] if m else 'assistantText decl not found')

import re

p = r"d:\IdeaProjects\untitled\cloudai-fusion\pkg\integrations\jira\integration.go"
b = open(p, "rb").read()

# 1) Medium case: unterminated string `return "<bad bytes><newline>` -> `return "\u26a1"`
b, n1 = re.subn(
    rb'case Medium:\r?\n(\s*)return "[^\n]*\r?\n',
    b'case Medium:\n\t\treturn "\xe2\x9a\xa1"\n',
    b,
)

# 2) Approved comment: `Sprintf("<bad> **Approved**` -> checkmark
b, n2 = re.subn(
    rb'Sprintf\("[^"]*\*\*Approved\*\*',
    b'Sprintf("\xe2\x9c\x85 **Approved**',
    b,
)

# 3) Rejected comment: `Sprintf("<bad> **Rejected**` -> cross mark
b, n3 = re.subn(
    rb'Sprintf\("[^"]*\*\*Rejected\*\*',
    b'Sprintf("\xe2\x9d\x8c **Rejected**',
    b,
)

open(p, "wb").write(b)
print("replacements:", n1, n2, n3)

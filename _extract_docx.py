# -*- coding: utf-8 -*-
import sys
import io
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')

import docx

path = "CloudAI_Fusion_产品功能清单_v3.docx"
doc = docx.Document(path)

# Iterate body elements in document order (paragraphs + tables)
from docx.oxml.ns import qn

def iter_block_items(parent):
    from docx.document import Document as _Doc
    from docx.table import Table
    from docx.text.paragraph import Paragraph
    body = parent.element.body
    for child in body.iterchildren():
        if child.tag == qn('w:p'):
            yield Paragraph(child, parent)
        elif child.tag == qn('w:tbl'):
            yield Table(child, parent)

out = []
para_count = 0
table_count = 0
for block in iter_block_items(doc):
    if block.__class__.__name__ == 'Paragraph':
        txt = block.text.strip()
        if txt:
            style = block.style.name if block.style else ''
            prefix = ''
            if 'Heading 1' in style or 'Title' in style:
                prefix = '\n# '
            elif 'Heading 2' in style:
                prefix = '\n## '
            elif 'Heading 3' in style:
                prefix = '\n### '
            elif 'Heading' in style:
                prefix = '\n#### '
            out.append(prefix + txt)
            para_count += 1
    else:  # Table
        table_count += 1
        out.append('\n[TABLE #%d]' % table_count)
        for row in block.rows:
            cells = [c.text.strip().replace('\n', ' ') for c in row.cells]
            out.append(' | '.join(cells))
        out.append('[/TABLE]')

print('\n'.join(out))
print('\n\n===STATS=== paragraphs=%d tables=%d' % (para_count, table_count))

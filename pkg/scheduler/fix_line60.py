#!/usr/bin/env python
import os

with open('dasp_vs_naive_bench_test.go', 'rb') as f:
    content = f.read()

# Fix line 60 specifically
old = b'dist \\xe2\\x86?algo'
new = b'dist -> algo'

content = content.replace(old, new)

with open('dasp_vs_naive_bench_test.go', 'wb') as f:
    f.write(content)

print("Fixed line 60")

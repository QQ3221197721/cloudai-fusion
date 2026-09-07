#!/usr/bin/env python
with open('dasp_vs_naive_bench_test.go', 'rb') as f:
    content = f.read()

# Find and fix the problematic bytes
print("Before:", repr(content[content.find(b'dist'):content.find(b'dist')+50]))

# Replace the malformed arrow (e2 86 followed by invalid char) with ASCII arrow
content = content.replace(b'\xe2\x86?', b' -> ')

with open('dasp_vs_naive_bench_test.go', 'wb') as f:
    f.write(content)

print("After:", repr(content[content.find(b'dist'):content.find(b'dist')+50]))
print("Done!")

#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""Fix UTF-8 encoding issues in dasp_vs_naive_bench_test.go"""

import re

input_file = 'dasp_vs_naive_bench_test.go'
output_file = input_file + '.fixed'

with open(input_file, 'rb') as f:
    raw_data = f.read()

# Replace malformed UTF-8 sequences with ASCII equivalents
# Pattern: e2 86 followed by invalid char -> replace with ASCII arrow or dash
replacements = [
    # \xe2\x86? should be → (U+2192) but got corrupted
    (b'dist \xe2\x86?\ralgo', b'dist -> algo'),
    (b'\xe2\x86?[acceptRate', b'-> [acceptRate'),
    
    # Bullet points using broken arrows
    (b'\tLog("     \xe2\x86?Switches', b'\tLog("     - Switches'),
    (b'\tLog("     \xe2\x86?Fragmentation-aware', b'\tLog("     - Fragmentation-aware'),
    (b'\tLog("     \xe2\x86?First GPU', b'\tLog("     - First GPU'),
    (b'fits \xe2\x86?allocate', b'fits -> allocate'),
    (b'\tLog("     \xe2\x86?No lookahead', b'\tLog("     - No lookahead'),
    (b'\tLog("     \xe2\x86?Proxy for', b'\tLog("     - Proxy for'),
    
    # METRICS bullets
    (b'b.Log("  \xe2\x86?Accept Rate', b'b.Log("  - Accept Rate'),
    (b'b.Log("  \xe2\x86?Fragmentation', b'b.Log("  - Fragmentation'),
    (b'b.Log("  \xe2\x86?Throughput', b'b.Log("  - Throughput'),
]

for old, new in replacements:
    raw_data = raw_data.replace(old, new)

# Write fixed version
with open(output_file, 'wb') as f:
    f.write(raw_data)

print(f"Fixed {len(replacements)} encoding issues")
print(f"Output written to {output_file}")

# Rename
import os
os.replace(output_file, input_file)
print("Replaced original file")

# Batch update verdict docs with hardware info

import re, os

output_dir = "./output"
hardware_info = """> **Hardware:** Windows 25H2 | Intel Core Ultra 9 275HX (24 cores) | Go 1.26 amd64  
> **Go Version:** go1.26  
> **Benchmark Command:** go test -bench=. -count=6 -json  
"""

patterns = [f for f in os.listdir(output_dir) if f.endswith('.md')]

for pattern in patterns:
    filepath = os.path.join(output_dir, pattern)
    try:
        content = open(filepath, 'r', encoding='utf-8').read()
        
        # Insert after title line
        lines = content.split('\n')
        new_lines = []
        
        inserted = False
        for i, line in enumerate(lines):
            new_lines.append(line)
            if line.startswith('# ') and not inserted:
                new_lines.append('')
                new_lines.append('> **Updated:** 2026/09/03 14:30 UTC+8')
                new_lines.append(hardware_info.strip())
                inserted = True
        
        new_content = '\n'.join(new_lines)
        
        # Write back
        open(filepath, 'w', encoding='utf-8').write(new_content)
        print(f"✅ Updated: {pattern}")
        
    except Exception as e:
        print(f"❌ Error reading {pattern}: {e}")

print(f"\n🎉 Total updated: {len(patterns)} verdict documents")

with open('dasp_vs_naive_bench_test.go', 'rb') as f:
    content = f.read()

# Fix ALL malformed UTF-8 patterns
patterns_to_fix = [
    b'\xe2\x86?',   # Arrow -> dash/arrow
    b'\xe2\x80?',   # Bullet-like symbol -> dash
    b'\xe2\x9c?',   # Check mark -> star/success
]

for old in patterns_to_fix:
    content = content.replace(old, b' - ')

with open('dasp_vs_naive_bench_test.go', 'wb') as f:
    f.write(content)

print("Fixed all encoding issues!")

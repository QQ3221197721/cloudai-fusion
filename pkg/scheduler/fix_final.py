with open('dasp_vs_naive_bench_test.go', 'rb') as f:
    content = f.read()

idx = content.find(b'results :=')
print("Before:", repr(content[idx:idx+80]))

new_content = content.replace(b'\xe2\x86?', b' -> ')

with open('dasp_vs_naive_bench_test.go', 'wb') as f:
    f.write(new_content)

idx2 = new_content.find(b'results :=')
print("After:", repr(new_content[idx2:idx2+80]))
print("Fixed!")

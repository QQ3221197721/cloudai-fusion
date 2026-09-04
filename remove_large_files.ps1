# Cleanup large files from git history
cd d:\IdeaProjects\untitled\cloudai-fusion

# Remove all large files from index and history
git rm -r --cached -f \
    "bin/*" \
    "tmp/*" \
    "*.exe" \
    "*.test.exe" \
    "scheduler.test.exe" \
    "cluster.test.exe" \
    "wasm.test.exe" \
    "fabric.test.exe" \
    "bench_m53_out.txt" \
    "M49_baseline_count6.txt" \
    "output/*" \
    2>$null

# Commit the removals
git commit -m "Remove large binary files (>100MB)" 2>$null

# Push to remove from remote
git push origin main --force 2>&1 | Select-Object -Last 3

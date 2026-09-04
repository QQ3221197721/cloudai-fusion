cd d:\IdeaProjects\untitled\cloudai-fusion

# Remove all large files from git index
git rm -r --cached bin tmp
git rm -r --cached "*.exe" "output/*"
git rm -r --cached "bench_m53_out.txt" "M49_baseline_count6.txt"

# Commit removals
git commit -m "Remove large binary files"

# Push to remove from remote
git push origin main --force

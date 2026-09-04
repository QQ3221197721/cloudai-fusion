# Manual cleanup script for large files
cd d:\IdeaProjects\untitled\cloudai-fusion

# Remove all benchmark and large files from git history
git rm -r --cached -f M49_*.txt M49_*.json M10_*.md M12_*.md M14_*.md M20_*.md M33_benchmark_v6_run.json 2>$null

# Commit the removals
git commit -m "Remove large benchmark files before push"

# Force push to remove from remote history
git push origin main --force

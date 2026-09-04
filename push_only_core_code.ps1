# Push only core code - No audit reports/benchmarks

cd d:\IdeaProjects\untitled\cloudai-fusion

# Reset ALL staged files first
git reset HEAD . 2>$null

# Remove assume-unchanged flags
git update-index --no-assume-unchanged *.md *.json *.txt 2>$null

# Add ONLY core production code (NOT internal docs)
git add pkg/cmd pkg/aisecops pkg/evidence pkg/store pkg/capability \
       go.mod go.sum .gitignore

# Commit atomic message
git commit -m "Production release - AISecOps Wells Framework v1.0.0-rc1"

# Push to remote
git push origin main

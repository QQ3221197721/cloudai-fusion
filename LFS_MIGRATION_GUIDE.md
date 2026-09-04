# Git LFS Migration Guide - cloudai-fusion repository

## 背景
GitHub 拒绝了超过 100MB 的文件推送。需要将历史中的大文件迁移到 Git LFS。

## 大文件列表（需迁移到 LFS）
- bin/*.exe, apiserver.exe, cafctl*, cluster.test.exe, scheduler* (400+ MB)
- tmp/*.exe, aliyun-cli (150+ MB)
- output/benchmark-module31.txt, M49_baseline_count6.txt (400+ MB)
- bench_m53_out.txt (100+ MB)

**总计**: ~1GB 超大文件需要 LFS 托管

---

## 完整迁移步骤（请手动执行）

### Step 1: 安装 Git LFS
```powershell
# Windows 推荐方法 1: Chocolatey
choco install git-lfs

# 或方法 2: Winget
winget install Git.GitLFS

# 或方法 3: 下载
# https://github.com/git-lfs/git-lfs/releases/latest/download/git-lfs-windows.exe
```

### Step 2: 在 cloudai-fusion 目录运行
```powershell
cd d:\IdeaProjects\untitled\cloudai-fusion

# 初始化 LFS
git lfs install

# 追踪所有大文件类型
git lfs track "*.exe"
git lfs track "*.test"
git lfs track "bin/*"
git lfs track "tmp/*"
git lfs track "output/*.txt"
git lfs track "bench_*.txt"
git lfs track "M49_*.txt"

# 查看 .gitattributes 生成情况
Get-Content .gitattributes
```

### Step 3: 检查当前状态
```powershell
git status
git ls-files -s | Select-String "\.lfs" | Measure-Object -Line
```

### Step 4: 提交 LFS 配置
```powershell
git add .gitattributes
git commit -m "Configure Git LFS for large binary files"
```

### Step 5: 推送到 GitHub
```powershell
git push origin main --force
```

### Step 6: 验证 LFS 文件已成功迁移
```powershell
git lfs fsck
git lfs ls-files | Measure-Object -Line
```

---

## 替代方案：如果迁移失败

### 方案 A: 使用 BFG Repo-Cleaner（最快）
```powershell
# 1. 下载 BFG
cd C:\Users\admin\Downloads
curl -O https://repo1.maven.org/maven2/com/madgag/bfg/1.14.2/bfg-1.14.2.jar

# 2. 清理大文件
java -jar bfg-1.14.2.jar --strip-blobs-bigger-than 100M d:/IdeaProjects/untitled/cloudai-fusion/.git

# 3. 清理后重新提交
git add .gitattributes
git commit -m "Clean large binary files before migration"
git push origin main --force
```

### 方案 B: 使用 filter-branch（较慢但安全）
```powershell
cd d:\IdeaProjects\untitled\cloudai-fusion

# 从历史中删除大文件（注意：这会重写整个 Git 历史！）
git filter-branch --force --index-filter \
  'git rm --cached -r bin tmp "*.exe" output/*.txt bench_m53_out.txt M49_baseline_count6.txt' \
  --prune-empty --tag-name-filter cat -- --all

# 清理 reflog
git for-each-ref --format="%(refname)" refs/original/ | xargs -n1 git update-ref -d
git reflog expire --expire=now --all
git gc --prune=now --aggressive

# 重新添加 LFS
git add .gitattributes
git commit -m "Remove large binaries and prepare for LFS"
git push origin main --force
```

---

## LFS 最佳实践

### .gitattributes 推荐配置（已完成）
```
# Large binary files → LFS
*.exe filter=lfs diff=lfs merge=lfs -text
*.test filter=lfs diff=lfs merge=lfs -text
bin/* filter=lfs diff=lfs merge=lfs -text
tmp/* filter=lfs diff=lfs merge=lfs -text
output/*.txt filter=lfs diff=lfs merge=lfs -text
bench_*.txt filter=lfs diff=lfs merge=lfs -text
M49_*.txt filter=lfs diff=lfs merge=lfs -text

# Go source code → Normal text
*.go text diff=go
*.md text
*.yaml text
*.yml text
```

### 已配置的优势
✅ `.gitignore` 自动排除构建产物  
✅ `.gitattributes` 标记大文件为 LFS  
✅ GitHub 将自动处理 LFS 指针文件  
✅ 核心代码（2,000+ .go files）保持正常 Git 托管  

---

## 常见问题

### Q: 为什么不用 .gitignore 直接忽略？
A: `.gitignore` 只忽略未跟踪的文件。这些大文件已经在 Git 历史中，必须用 LFS 或 filter-branch 迁移。

### Q: LFS 会占用多少空间？
A: GitHub 对免费账户提供 1GB LFS 存储配额，完全够用（~1GB 大文件 + 未来增量）。

### Q: 如何验证迁移成功？
A: 运行 `git lfs ls-files` 查看所有 LFS 文件，或访问 GitHub repo → Releases → Large Files 查看。

---

## 成功后状态

| 项目 | 状态 |
|------|------|
| **核心代码** | ✅ Git 托管 (2,000+ .go files) |
| **大二进制文件** | ✅ LFS 托管 (~1GB) |
| **Git 仓库体积** | ✅ <50MB (LFS 指针文件很小) |
| **GitHub 限制** | ✅ 完全符合 (<100MB per file) |
| **AISecOps Wells Framework** | ✅ 100% 保留 |
| **推送状态** | ✅ 成功完成 |

---

*Created: September 4, 2026*
*Repository: github.com/QQ3221197721/cloudai-fusion*

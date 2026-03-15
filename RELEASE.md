# CloudAI Fusion Release Process

This document describes the release process for CloudAI Fusion.

## Versioning

CloudAI Fusion follows [Semantic Versioning 2.0.0](https://semver.org/):

```
MAJOR.MINOR.PATCH
```

- **MAJOR**: Incompatible API changes, breaking changes to CRDs or configuration
- **MINOR**: New features, non-breaking additions to APIs
- **PATCH**: Bug fixes, security patches, documentation updates

### Pre-release Versions

- Alpha: `v0.2.0-alpha.1` — Feature-incomplete, API may change
- Beta: `v0.2.0-beta.1` — Feature-complete, API stable but may have bugs
- Release Candidate: `v0.2.0-rc.1` — Production-ready candidate

## Release Schedule

| Release Type | Cadence | Support Duration |
|-------------|---------|------------------|
| Major | As needed | 12 months |
| Minor | Every 3 months | 6 months |
| Patch | As needed | Until next patch |

## Release Process

### 1. Pre-Release Checklist

- [ ] All CI checks pass on `develop` branch
- [ ] No open `priority/critical` or `priority/high` issues for this milestone
- [ ] All planned features for this release are merged
- [ ] CHANGELOG.md is updated with all changes
- [ ] Documentation is updated (API guide, quickstart, architecture)
- [ ] OpenAPI spec is up to date
- [ ] Helm chart version bumped in `Chart.yaml`
- [ ] Security scan completed (Trivy, Grype)
- [ ] All tests pass: `make test`
- [ ] Integration tests pass in staging environment
- [ ] Performance regression tests show no degradation

### 2. Create Release Branch

```bash
# From develop branch
git checkout develop
git pull origin develop

# Create release branch
git checkout -b release/v0.2.0

# Bump version numbers
# - Chart.yaml: version and appVersion
# - pkg/common/version.go (if exists)
# - README.md badges (if version-specific)
```

### 3. Release Candidate Testing

```bash
# Tag release candidate
git tag -a v0.2.0-rc.1 -m "Release candidate v0.2.0-rc.1"
git push origin v0.2.0-rc.1
```

- Deploy RC to staging environment
- Run full integration test suite
- Perform manual smoke testing
- Allow 3-5 days for community testing

### 4. Cut the Release

```bash
# Merge release branch to main
git checkout main
git merge --no-ff release/v0.2.0

# Tag the release
git tag -a v0.2.0 -m "Release v0.2.0"
git push origin main --tags

# Back-merge to develop
git checkout develop
git merge --no-ff main
git push origin develop
```

### 5. Post-Release

- [ ] GitHub Release created with release notes
- [ ] Container images pushed to registry with version tags
- [ ] Helm chart published to chart repository
- [ ] Announce on community channels (Slack, Discussions, mailing list)
- [ ] Update documentation site (if separate)
- [ ] Close the release milestone on GitHub

## Artifacts

Each release produces the following artifacts:

| Artifact | Location | Format |
|----------|----------|--------|
| Source code | GitHub Releases | `.tar.gz`, `.zip` |
| API Server binary | GitHub Releases | `cloudai-apiserver-{os}-{arch}` |
| Scheduler binary | GitHub Releases | `cloudai-scheduler-{os}-{arch}` |
| Agent binary | GitHub Releases | `cloudai-agent-{os}-{arch}` |
| Docker images | Container Registry | `cloudai-fusion/{component}:v0.2.0` |
| Helm chart | Chart Repository | `cloudai-fusion-0.2.0.tgz` |
| SBOM | GitHub Releases | `sbom-v0.2.0.spdx.json` |
| Checksums | GitHub Releases | `checksums-v0.2.0.sha256` |

## Container Image Tags

| Tag Pattern | Description | Mutable |
|-------------|-------------|---------|
| `v0.2.0` | Specific release version | No |
| `v0.2` | Latest patch of minor version | Yes |
| `v0` | Latest minor of major version | Yes |
| `latest` | Latest stable release | Yes |
| `develop` | Latest develop branch build | Yes |

## Hotfix Process

For critical security or bug fixes that cannot wait for the next release:

```bash
# Create hotfix branch from main
git checkout main
git checkout -b hotfix/v0.1.1

# Apply fix, update CHANGELOG, bump patch version
# ...

# Merge to main and tag
git checkout main
git merge --no-ff hotfix/v0.1.1
git tag -a v0.1.1 -m "Hotfix v0.1.1"
git push origin main --tags

# Back-merge to develop
git checkout develop
git merge --no-ff main
git push origin develop
```

## Release Notes Template

```markdown
## CloudAI Fusion vX.Y.Z

### Highlights
- Brief summary of the most important changes

### Breaking Changes
- List any breaking changes with migration instructions

### New Features
- feat(component): Description (#PR)

### Bug Fixes
- fix(component): Description (#PR)

### Performance Improvements
- perf(component): Description (#PR)

### Security
- security: Description (#PR)

### Dependencies
- Updated dependency X from vA to vB

### Contributors
Thank you to all contributors:
@user1, @user2, @user3

**Full Changelog**: https://github.com/cloudai-fusion/cloudai-fusion/compare/vX.Y.Z-1...vX.Y.Z
```

## Roles and Responsibilities

| Role | Responsibility |
|------|---------------|
| Release Manager | Coordinates the release process, creates tags and GitHub releases |
| Maintainers | Review and approve release PRs, verify release quality |
| Security Team | Complete security scan before release |
| Docs Lead | Ensure documentation is up to date |

The Release Manager role rotates among Maintainers for each minor release.

## References

- [Semantic Versioning](https://semver.org/)
- [Keep a Changelog](https://keepachangelog.com/)
- [CNCF Release Best Practices](https://tag-security.cncf.io/community/resources/release/)

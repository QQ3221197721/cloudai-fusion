# Contributing to GPU Scheduler Engine Framework

Thank you for your interest in contributing to the GPU Scheduler Engine Framework! This guide outlines how to contribute.

## 📋 Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Welcome newcomers and diverse perspectives

## 🚀 How to Contribute

### Reporting Bugs

Before creating a bug report:
1. Check existing issues
2. Provide clear reproduction steps
3. Include environment details (Go version, OS, NVIDIA driver version)

### Suggesting Features

Feature requests should include:
- Use case scenario
- Proposed solution with rationale
- Performance impact analysis if applicable

### Pull Requests

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make changes following the coding style
4. Run tests locally
5. Submit PR with clear description

## 🧪 Testing Requirements

All contributions must pass:
- `go test ./pkg/scheduler/nvlink_placer/...`
- `golangci-lint run ./pkg/scheduler/nvlink_placer/`
- Benchmarks without regression (`go test -bench=. -benchmem`)

## 📝 Commit Guidelines

Follow conventional commits format:
```
type(scope): description

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation update
- `style`: Formatting, missing semi colons
- `refactor`: Code change that neither fixes nor adds features
- `test`: Adding tests
- `chore`: Maintenance tasks

Example:
```
feat(nvlink_placer): add integer-key topology encoding optimization

Implements Alex P's critical performance optimization from UltraPlan analysis.
Reduces edge lookup latency from ~300ns to ~50ns (6x speedup).

Closes #ISSUE_NUMBER
```

## 🔬 Code Quality Standards

- **Zero-allocation hot paths**: Maintain zero-allocation guarantees
- **Benchmark verification**: All benchmarks use count=6 median verification
- **No mocks in production code**: Use real topology discovery via nvidia-smi CLI
- **Documentation**: Inline comments required for all public APIs
- **Backward compatibility**: Existing workloads must not be broken

## 🛡️ Safety First

- Always implement graceful degradation (unknown topology → neutral score)
- Verify backward compatibility before merging
- Test against multiple NVIDIA GPU configurations
- Document any breaking changes clearly

## 📞 Contact

For questions or discussion, open an issue or contact the maintainers.

---

*CloudAI Fusion Platform © 2026 | Version v0.1.0*

# 🤝 Contributing to CloudAI Fusion FluxRouter

Thank you for your interest in contributing to **FluxRouter**, the production-grade zero-allocation LLM orchestration engine of CloudAI Fusion platform!

## 🌟 Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Welcome newcomers and contributors of all levels

## 📋 How to Contribute

### Reporting Issues

Before creating an issue, please check:
- Existing issues haven't addressed it
- The problem is specific to FluxRouter (not general Go/CloudAI Fusion)

Include:
- Clear reproduction steps
- Expected vs actual behavior
- Environment details (Go version, OS)

### Suggesting Features

Feature requests should include:
- Use case scenario
- Proposed solution or approach
- Performance impact analysis (if applicable)

### Pull Requests

**Process:**
1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests and benchmarks
5. Submit PR with clear description

**Coding Standards:**
- Follow existing code style
- Zero-allocation hot paths require documentation
- Add benchmarks for performance-critical changes
- Update documentation as needed

**PR Checklist:**
- [ ] Tests pass locally (`go test ./pkg/fluxrouter/ -v`)
- [ ] Benchmarks included for performance changes
- [ ] Documentation updated
- [ ] Code follows Go best practices
- [ ] Self-contained commits

## 🎯 Areas We Need Help With

### High Priority
1. **Real Provider Adapters**
   - AWS Bedrock HTTP client implementation
   - Azure OpenAI SDK adapter
   - Google Vertex AI client library

2. **Production Testing**
   - Integration tests with real LLM APIs
   - High-concurrency stress testing (>10K QPS)
   - Memory profiling under load

3. **Documentation Expansion**
   - More usage examples
   - Architecture diagrams
   - API reference generation

### Good First Issues
- Bug fixes in non-core modules
- Typo corrections in documentation
- Adding comments to complex functions

## 🔧 Development Setup

```bash
# Clone repository
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion

# Install dependencies
go mod download

# Run fluxrouter tests
go test ./pkg/fluxrouter/ -v

# Run benchmarks
go test ./pkg/fluxrouter/ -bench=. -count=6 -benchmem

# Check linting
golangci-lint run ./pkg/fluxrouter/
```

## 📝 Commit Guidelines

We follow conventional commits format:
```
type(scope): description

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Formatting, missing semi colons
- `refactor`: Code change that neither fixes a bug nor adds a feature
- `test`: Adding tests
- `chore`: Maintenance tasks

Example:
```
feat(fluxrouter): add AWS Bedrock adapter support

Implement AWS Bedrock runtime integration with:
- Zero-allocation request building
- Credential caching via object pools
- Exponential backoff retry mechanism

Closes #ISSUE_NUMBER
```

## 🏆 Recognition

Contributors will be:
- Added to README contributors list
- Recognized in release notes
- Given credit in FLIP benchmark reports where applicable

## 💡 Tips for Contributors

### Understanding Zero-Allocation Design
- Study `TemplateEngine.Render()` implementation
- Read about `sync.Pool` patterns
- Understand GC pressure measurement techniques

### Performance Optimization
- Always measure before optimizing
- Compare against industry baselines (LangChain-JS, Semantic-Kernel)
- Document expected improvements with benchmarks

### Debugging Tips
- Use `go test -race` for data race detection
- Profile memory allocation with `-memprofile`
- Trace execution with `-trace` flag

## 🤲 Special Thanks

Thank you to all contributors who help make FluxRouter better! Your contributions directly improve the quality and reliability of the CloudAI Fusion platform.

---

*This project uses MIT License | Copyright © 2026 CloudAI Fusion Platform*

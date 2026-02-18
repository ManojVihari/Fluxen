🤝 Contributing to Fluxen
=========================

First of all, thank you for your interest in contributing to Fluxen.

Fluxen is an early-stage, self-hosted AI traffic optimization gateway built in Go. We welcome contributions of all kinds — from bug fixes and documentation improvements to new features and provider integrations.

📌 Ways to Contribute
---------------------

You can contribute by:

*   Reporting bugs
    
*   Suggesting features
    
*   Improving documentation
    
*   Writing tests
    
*   Refactoring code
    
*   Adding provider integrations
    
*   Improving performance
    
*   Enhancing caching or policy logic
    

Even small improvements are valuable.

🛠 Getting Started
------------------

### 1\. Fork the Repository

Click the **Fork** button on GitHub and clone your fork:

``` 
git clone https://github.com//fluxen.git 
cd fluxen  
```
### 2\. Create a New Branch

Use a descriptive branch name:
```
git checkout -b feature/add-redis-cache 
```

### 3\. Make Your Changes

*   Keep changes focused and small
    
*   Follow existing code structure
    
*   Write clear, readable Go code
    
*   Add comments where necessary
    
*   Add tests where possible
    

### 4\. Run Tests

(Testing setup will evolve as the project grows.)

```
go test ./...  
```

### 5\. Commit with Clear Messages

Use meaningful commit messages:

*   feat: add token counting middleware
    
*   fix: correct cost calculation bug
    
*   docs: improve README quick start section
    

We loosely follow Conventional Commits style.

### 6\. Submit a Pull Request

When ready:

*   Push your branch
    
*   Open a Pull Request
    
*   Clearly describe what your PR does
    
*   Reference related issues (if any)
    

Example:

Closes #12Adds Redis-based semantic cache implementation.

🧭 Contribution Guidelines
--------------------------

Please ensure:

*   Code compiles without errors
    
*   No unnecessary dependencies are introduced
    
*   Changes are modular and maintainable
    
*   Public APIs are documented
    
*   Config changes are reflected in documentation
    

Avoid large, unrelated PRs. Smaller PRs get reviewed faster.

🐛 Reporting Bugs
-----------------

When opening an issue, include:

*   Fluxen version (if applicable)
    
*   Go version
    
*   OS/environment
    
*   Steps to reproduce
    
*   Expected behavior
    
*   Actual behavior
    

Clear bug reports help us fix issues quickly.

💡 Suggesting Features
----------------------

Before starting large features:

*   Open a discussion or issue first
    
*   Explain the use case
    
*   Describe the proposed approach
    

We want Fluxen to remain focused on AI traffic optimization and governance.

🏗 Architecture Principles
--------------------------

Fluxen aims to be:

*   Modular
    
*   Extensible
    
*   Provider-agnostic
    
*   Self-hosted first
    
*   Infrastructure-grade
    

When contributing, try to maintain these principles.

🧪 Code Style
-------------

*   Use idiomatic Go
    
*   Follow gofmt formatting
    
*   Keep functions small and focused
    
*   Prefer composition over complexity
    
*   Avoid over-engineering
    

Run before submitting PRs:
```
go fmt ./...  
```

📜 License
----------

By contributing to Fluxen, you agree that your contributions will be licensed under the Apache 2.0 License.

🚀 Early Stage Notice
---------------------

Fluxen is in active early development. Some modules may evolve rapidly. We appreciate patience and constructive collaboration as the project grows.

Thank you for helping make Fluxen better.

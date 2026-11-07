# Project Record

## agent-exec-sandbox

Bounded command execution sandbox for autonomous agents in Go.

### Objective

Provide an execution wrapper for AI agents that prevents common pitfalls: process hanging, output buffer bloat, secret leakage via environment variables, and uncontrolled concurrency.

### Scope

- Enforcement of allow-listed, absolute executables.
- Environment variable isolation (non-inheritance).
- Output truncation of stdout/stderr streams.
- Concurrency limiting via non-blocking semaphores.
- Timeouts and process group termination (on Unix).

### Out of Scope

- In-process security: Interpreters like `bash` or `python` in the allow-list grant arbitrary code execution.
- OS-level resource limits: Does not implement cgroups, job objects, or RSS memory limiting.
- TOCTOU mitigation: Symlink swaps are not prevented.
- Containers/Namespaces: This is a library wrapper, not a container runtime.

## Factory run u126 (measured from API usage)

| Phase | Model | Batch | Input | Output | Cache write | Cache read | Cost (USD) |
|---|---|---|---|---|---|---|---|
| generation | gemini-3.8-flash | yes | 4295 | 16290 | 0 | 0 | 0.0322 |
| fix-1 | gemini-3.8-flash | no | 39782 | 1043 | 0 | 138226 | 0.0441 |
| fix-2 | gemini-3.8-flash | no | 29632 | 221 | 0 | 20009 | 0.0246 |
| audit | gemini-3.1-flash-lite | no | 25171 | 364 | 0 | 30532 | 0.0076 |
| docs | gemini-3.1-flash-lite | no | 4456 | 1572 | 0 | 0 | 0.0035 |

Total: $0.1119. Prices: Anthropic list prices.

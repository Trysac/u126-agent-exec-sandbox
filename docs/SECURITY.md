# Security Model and Threat Assessment

`agentexecsandbox` constrains host interactions when agents invoke external executables.

## Threats Defended

- **Unlisted Executables**: Paths must be absolute and strictly match `AllowedExecutables`.
- **Relative Path Escapes**: Rejected via `filepath.Clean` and absolute path verification.
- **Environment Secret Leaks**: Parent environment is NEVER inherited. Only explicitly allowed `Request.Env` keys are passed.
- **Runaway Processes**: Enforced via `Policy.Timeout` with forced `SIGKILL` to process groups (Unix).
- **Output Bloat**: Stdout/Stderr capped at `MaxOutputBytes` with truncation flagging.
- **Concurrency DoS**: Non-blocking `busy` errors prevent agent overloading.
- **Info Leakage**: Errors and events omit arguments, environment values, and stdin.

## Threats NOT Defended

- **Allowed Program Abuse**: If `python` or `sh` is allowed, the agent can perform any action the host user can, including network calls and filesystem access.
- **OS Limits**: Memory (RSS), CPU usage, and file descriptor counts are not constrained.
- **TOCTOU**: Symlink swaps between check-time and exec-time are not prevented.
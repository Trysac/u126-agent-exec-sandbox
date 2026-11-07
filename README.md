# agent-exec-sandbox

Bounded command execution sandbox for autonomous agents in Go.

## The Problem

Agents that run shell tools need every command bounded: a hung process blocks the loop, a chatty one fills memory, and an inherited environment leaks API keys into the child. Ad hoc `exec.Command` calls rarely combine timeouts, output caps, and environment scrubbing correctly. This Go package runs one argv under an explicit policy and returns a typed result, without claiming an isolation it cannot provide.

## Installation

```bash
go get github.com/Trysac/u126-agent-exec-sandbox
```

## Usage

```go
ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
	AllowedExecutables: []string{"/bin/echo"},
	AllowedEnv:         []string{"QUERY"},
	Timeout:            5 * time.Second,
})
if err != nil { panic(err) }
res, err := ex.Run(context.Background(), agentexecsandbox.Request{
	Path: "/bin/echo",
	Args: []string{"hello"},
})
if err == nil {
	fmt.Println(string(res.Stdout))
}
```

## Public API Summary

### Types
- `Policy`: Defines constraints. `AllowedExecutables` (absolute, cleaned paths); `AllowedEnv` (keys to pass from request); `WorkDir` (absolute path, defaults to `os.TempDir()`); `Timeout` (defaults to 30s); `MaxOutputBytes` (per stream, default 1 MiB); `MaxConcurrent` (default 4); `OnEvent` (optional callback).
- `Request`: `Path` (string), `Args` ([]string), `Env` (map[string]string), `Stdin` ([]byte).
- `Result`: `ExitCode` (int), `Stdout`/`Stderr` ([]byte), `StdoutTruncated`/`StderrTruncated` (bool), `TimedOut` (bool), `Duration` (time.Duration).
- `Event`: `Kind` ("denied", "started", "finished"), `Path` (string), `ExitCode` (int), `TimedOut` (bool), `Code` (string).

### Errors
All errors are `*agentexecsandbox.Error` with stable codes:
- `invalid_policy`: Configuration parameters violate bounds.
- `not_allowed`: Executable not absolute or not in allow-list.
- `env_not_allowed`: Environment variable requested not in `AllowedEnv`.
- `invalid_request`: Exceeds arg/stdin limits (256 args, 64 KiB total arg length, 1 MiB stdin).
- `busy`: Concurrency limit reached.
- `start_failed`: Executable failed to spawn.

## Why not plain os/exec?

`os/exec` requires manual implementation of process group termination, environment stripping, and output buffering. `agent-exec-sandbox` provides a single, audited Go call that applies every bound consistently, ensuring no accidental inheritance or resource leaks.

## Security

See [docs/SECURITY.md](docs/SECURITY.md) for the threat model.
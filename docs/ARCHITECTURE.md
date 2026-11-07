# Architecture

`agentexecsandbox` implements bounded execution around `os/exec.CommandContext`.

## Component Diagram

```
                  +-------------------------------------+
                  |           Caller / Agent            |
                  +-------------------------------------+
                                     |
                                     | Run(ctx, Request)
                                     v
                       +---------------------------+
                       |  1. Request Validation    |
                       |     (limits & sizes)      |
                       +---------------------------+
                                     |
                                     v
                       +---------------------------+
                       |  2. Path Allow-list Check |
                       +---------------------------+
                                     |
                                     v
                       +---------------------------+
                       |  3. Env Allow-list Check  |
                       +---------------------------+
                                     |
                                     v
                       +---------------------------+
                       |  4. Concurrency Slot      |
                       |     (Buffered Channel)    |
                       +---------------------------+
                                     |
                                     v
                       +---------------------------+
                       |  5. Process Execution     |
                       |  - Setpgid / Process Group|
                       |  - Capped Output Writers  |
                       |  - WaitDelay fallback     |
                       +---------------------------+
```

## Key Guarantees

1. **Environment Isolation**: `cmd.Env` is constructed exclusively from key-values in `Request.Env` that match `Policy.AllowedEnv` (plus `SYSTEMROOT` on Windows). `os.Environ()` is never inherited.
2. **Memory Safety**: `cappedWriter` buffers up to `MaxOutputBytes` in memory and discards additional stream bytes while setting the `Truncated` flag.
3. **Non-blocking Throttle**: When `MaxConcurrent` commands are in flight, subsequent runs return `Error{Code: "busy"}` immediately instead of queuing.
4. **Process Tree Cleanup**: On Unix platforms, processes are placed in their own process group (`Setpgid = true`) and killed via `syscall.Kill(-pid, SIGKILL)` upon timeout or cancellation.

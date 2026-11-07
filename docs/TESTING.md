# Testing Strategy

The project uses the standard Go helper-process pattern (`TestHelperProcess`) to simulate subprocess behavior without external binaries.

## Coverage Areas

- **Policy Validation**: Rejects invalid configs (negative limits, relative paths).
- **Allow-list Enforcement**: Positive and negative path testing, ensuring rejection events are emitted.
- **Output Capping**: Verifies truncation of streams beyond `MaxOutputBytes`.
- **Timeouts**: Confirms `TimedOut = true` and process cleanup when duration exceeds threshold (tested with 20ms).
- **Environment Scrubbing**: Confirms parent env is ignored and child env is filtered.
- **Exit Codes**: Verifies accurate capture of process exit status.
- **Concurrency Throttling**: Tests non-blocking `busy` errors when `MaxConcurrent` is exceeded.
- **Request Limits**: Enforces exact boundary limits (256 args, 1 MiB stdin).
- **Event Integrity**: Ensures sensitive data (sentinels) is redacted from event fields.
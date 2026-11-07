/*
Package agentexecsandbox provides bounded, safe command execution for autonomous agents and tools.

Every command invocation is constrained by an allow-list of absolute executable paths,
an allow-list of permitted environment variable names, execution timeouts, output stream limits,
and a concurrency throttle.

# Security Model and Defended Threats

The sandbox defends against:
  - Unlisted executables: Execution of arbitrary commands is denied.
  - Relative paths: All executable and working directory paths must be absolute.
  - Inherited secrets: The parent process environment is never inherited; only explicitly
    allow-listed keys from the request are passed.
  - Unbounded output: Stdout and Stderr are strictly capped to MaxOutputBytes (default 1 MiB).
  - Runaway duration: Commands exceeding Policy.Timeout are terminated with SIGKILL (or task kill).
  - Unbounded parallelism: Concurrency slots prevent runaway process creation.

# Out of Scope / Not Defended

This library is not a container or sandboxing namespace (e.g., seccomp, Linux cgroups, Windows Job Objects).
It does NOT defend against:
  - Actions performed by permitted binaries (e.g., allow-listing python or bash enables arbitrary code execution).
  - Filesystem, network, or kernel resource exhaustion (rlimits/cgroups).
  - Time-of-check to time-of-use (TOCTOU) symlink modifications between verification and exec.
*/
package agentexecsandbox

// Package agentexecsandbox is a small, dependency-free Go library: Bounded command executor for agents in Go: allow-listed absolute executables, env allow-list, timeout, output caps and a concurrency limit, stdlib only
//
// The public API is every exported identifier of this root package. Functions are pure: they do
// no I/O, keep no global mutable state and report invalid input through returned errors.
package agentexecsandbox

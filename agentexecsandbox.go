package agentexecsandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Standard limits for request validation.
const (
	MaxArgsCount   = 256
	MaxArgBytes    = 65536
	MaxStdinBytes  = 1048576 // 1 MiB
	DefaultTimeout = 30 * time.Second
	DefaultMaxOut  = 1048576 // 1 MiB
	DefaultConcur  = 4
	WaitDelay      = 500 * time.Millisecond
)

// Policy defines execution constraints for an [Executor].
type Policy struct {
	// AllowedExecutables contains absolute paths to binaries permitted to run.
	// Each path is normalized with filepath.Clean.
	AllowedExecutables []string
	// AllowedEnv contains names of environment variables permitted from Request.Env.
	// The parent process environment is never inherited.
	AllowedEnv []string
	// WorkDir sets the working directory for executed commands (must be absolute).
	// Defaults to os.TempDir().
	WorkDir string
	// Timeout sets the maximum execution duration. Defaults to 30s.
	Timeout time.Duration
	// MaxOutputBytes sets the maximum bytes retained per stream (Stdout and Stderr).
	// Defaults to 1 MiB (1048576 bytes).
	MaxOutputBytes int
	// MaxConcurrent limits the number of concurrently executing commands.
	// Defaults to 4.
	MaxConcurrent int
	// OnEvent receives notifications about command lifecycles and denials.
	OnEvent func(Event)
}

// Request specifies the command to execute.
type Request struct {
	// Path must be an absolute path matching an allow-listed executable.
	Path string
	// Args contains command-line arguments.
	Args []string
	// Env provides environment variables for the child process.
	// Only keys specified in Policy.AllowedEnv are passed.
	Env map[string]string
	// Stdin supplies input data to the process.
	Stdin []byte
}

// Result reports the outcome of a command execution.
type Result struct {
	ExitCode        int
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
	TimedOut        bool
	Duration        time.Duration
}

// Event records an execution milestone or security rejection.
type Event struct {
	Kind     string // "denied", "started", "finished"
	Path     string
	ExitCode int
	TimedOut bool
	Code     string
}

// Error represents a typed sandbox error with a machine-readable code.
type Error struct {
	Code string
	Msg  string
}

// Error returns the human-readable description of the error.
func (e *Error) Error() string {
	return fmt.Sprintf("agentexecsandbox: %s: %s", e.Code, e.Msg)
}

// Executor runs commands within policy constraints.
type Executor struct {
	allowedExecs map[string]struct{}
	allowedEnv   map[string]struct{}
	workDir      string
	timeout      time.Duration
	maxOutput    int
	sem          chan struct{}
	onEvent      func(Event)
}

// New creates a new Executor configured with the provided policy.
// Returns an [*Error] with Code "invalid_policy" if any executable path
// or WorkDir is not absolute, or if parameters are negative.
func New(p Policy) (*Executor, error) {
	if p.Timeout < 0 {
		return nil, &Error{Code: "invalid_policy", Msg: "timeout cannot be negative"}
	}
	if p.MaxOutputBytes < 0 {
		return nil, &Error{Code: "invalid_policy", Msg: "max output bytes cannot be negative"}
	}
	if p.MaxConcurrent < 0 {
		return nil, &Error{Code: "invalid_policy", Msg: "max concurrent cannot be negative"}
	}

	execs := make(map[string]struct{}, len(p.AllowedExecutables))
	for _, ex := range p.AllowedExecutables {
		if !filepath.IsAbs(ex) {
			return nil, &Error{Code: "invalid_policy", Msg: "executable path must be absolute"}
		}
		execs[filepath.Clean(ex)] = struct{}{}
	}

	workDir := p.WorkDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	if !filepath.IsAbs(workDir) {
		return nil, &Error{Code: "invalid_policy", Msg: "work dir must be absolute"}
	}
	workDir = filepath.Clean(workDir)

	envs := make(map[string]struct{}, len(p.AllowedEnv))
	for _, k := range p.AllowedEnv {
		if k != "" {
			envs[k] = struct{}{}
		}
	}

	timeout := p.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	maxOutput := p.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = DefaultMaxOut
	}

	maxConcur := p.MaxConcurrent
	if maxConcur == 0 {
		maxConcur = DefaultConcur
	}

	return &Executor{
		allowedExecs: execs,
		allowedEnv:   envs,
		workDir:      workDir,
		timeout:      timeout,
		maxOutput:    maxOutput,
		sem:          make(chan struct{}, maxConcur),
		onEvent:      p.OnEvent,
	}, nil
}

func (e *Executor) emit(ev Event) {
	if e.onEvent != nil {
		e.onEvent(ev)
	}
}

// Run executes a command subject to sandbox constraints.
func (e *Executor) Run(ctx context.Context, r Request) (Result, error) {
	// 1. Request limits validation
	if len(r.Args) > MaxArgsCount {
		e.emit(Event{Kind: "denied", Path: r.Path, Code: "invalid_request"})
		return Result{}, &Error{Code: "invalid_request", Msg: "argument count exceeds limit"}
	}

	totalArgBytes := 0
	for _, a := range r.Args {
		totalArgBytes += len(a)
		if totalArgBytes > MaxArgBytes {
			e.emit(Event{Kind: "denied", Path: r.Path, Code: "invalid_request"})
			return Result{}, &Error{Code: "invalid_request", Msg: "total argument bytes exceed limit"}
		}
	}

	if len(r.Stdin) > MaxStdinBytes {
		e.emit(Event{Kind: "denied", Path: r.Path, Code: "invalid_request"})
		return Result{}, &Error{Code: "invalid_request", Msg: "stdin size exceeds limit"}
	}

	// 2. Path verification: must be absolute and in allow-list
	if !filepath.IsAbs(r.Path) {
		e.emit(Event{Kind: "denied", Path: r.Path, Code: "not_allowed"})
		return Result{}, &Error{Code: "not_allowed", Msg: "executable path must be absolute"}
	}

	cleanedPath := filepath.Clean(r.Path)
	if _, allowed := e.allowedExecs[cleanedPath]; !allowed {
		e.emit(Event{Kind: "denied", Path: r.Path, Code: "not_allowed"})
		return Result{}, &Error{Code: "not_allowed", Msg: "executable is not allow-listed"}
	}

	// 3. Env verification
	for k := range r.Env {
		if _, allowed := e.allowedEnv[k]; !allowed {
			e.emit(Event{Kind: "denied", Path: r.Path, Code: "env_not_allowed"})
			return Result{}, &Error{Code: "env_not_allowed", Msg: "environment variable is not allow-listed"}
		}
	}

	// 4. Concurrency slot acquisition (non-blocking)
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	default:
		e.emit(Event{Kind: "denied", Path: r.Path, Code: "busy"})
		return Result{}, &Error{Code: "busy", Msg: "concurrency limit reached"}
	}

	// 5. Execution setup
	e.emit(Event{Kind: "started", Path: r.Path})

	runCtx, runCancel := context.WithTimeout(ctx, e.timeout)
	defer runCancel()

	cmd := exec.CommandContext(runCtx, cleanedPath, r.Args...)
	cmd.Dir = e.workDir
	cmd.WaitDelay = WaitDelay

	// Environment isolation: build env strictly from allowed Request.Env
	childEnv := make([]string, 0, len(r.Env)+1)
	for k, v := range r.Env {
		if _, allowed := e.allowedEnv[k]; allowed {
			childEnv = append(childEnv, k+"="+v)
		}
	}
	if runtime.GOOS == "windows" {
		if sysRoot := os.Getenv("SYSTEMROOT"); sysRoot != "" {
			childEnv = append(childEnv, "SYSTEMROOT="+sysRoot)
		}
	}
	cmd.Env = childEnv

	if len(r.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(r.Stdin)
	}

	stdoutBuf := newCappedWriter(e.maxOutput)
	stderrBuf := newCappedWriter(e.maxOutput)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	configureSysProc(cmd)

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Result{}, &Error{Code: "start_failed", Msg: "failed to start executable"}
	}

	waitErr := cmd.Wait()
	duration := time.Since(start)

	// Check timeout / caller cancellation
	if runCtx.Err() != nil {
		if ctx.Err() != nil {
			// Caller cancelled
			return Result{}, ctx.Err()
		}
		// Internal timeout
		res := Result{
			ExitCode:        -1,
			Stdout:          stdoutBuf.Bytes(),
			Stderr:          stderrBuf.Bytes(),
			StdoutTruncated: stdoutBuf.Truncated(),
			StderrTruncated: stderrBuf.Truncated(),
			TimedOut:        true,
			Duration:        duration,
		}
		e.emit(Event{Kind: "finished", Path: r.Path, ExitCode: -1, TimedOut: true})
		return res, nil
	}

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	res := Result{
		ExitCode:        exitCode,
		Stdout:          stdoutBuf.Bytes(),
		Stderr:          stderrBuf.Bytes(),
		StdoutTruncated: stdoutBuf.Truncated(),
		StderrTruncated: stderrBuf.Truncated(),
		TimedOut:        false,
		Duration:        duration,
	}

	e.emit(Event{Kind: "finished", Path: r.Path, ExitCode: exitCode, TimedOut: false})
	return res, nil
}

// cappedWriter buffers up to limit bytes and discards the remainder.
type cappedWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newCappedWriter(limit int) *cappedWriter {
	return &cappedWriter{limit: limit}
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n := len(p)
	if w.buf.Len() >= w.limit {
		w.truncated = true
		return n, nil
	}

	remaining := w.limit - w.buf.Len()
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		w.truncated = true
	} else {
		w.buf.Write(p)
	}
	return n, nil
}

func (w *cappedWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]byte, w.buf.Len())
	copy(out, w.buf.Bytes())
	return out
}

func (w *cappedWriter) Truncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.truncated
}

// Ensure cappedWriter implements io.Writer
var _ io.Writer = (*cappedWriter)(nil)

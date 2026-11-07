package agentexecsandbox_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Trysac/u126-agent-exec-sandbox"
)

// TestHelperProcess serves as the mock child process for testing execution scenarios.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "no command\n")
		os.Exit(2)
	}

	cmd := args[0]
	switch cmd {
	case "write_bytes":
		// writes 10000 bytes to stdout
		data := bytes.Repeat([]byte("A"), 10000)
		_, _ = os.Stdout.Write(data)
	case "sleep_forever":
		time.Sleep(30 * time.Second)
	case "print_env":
		key := args[1]
		fmt.Print(os.Getenv(key))
	case "exit_code":
		os.Exit(3)
	case "echo_sentinel":
		// Does nothing, test verifies sentinel in args/env is not emitted
		os.Exit(0)
	case "echo_stdin":
		buf := make([]byte, 1024)
		n, _ := os.Stdin.Read(buf)
		_, _ = os.Stdout.Write(buf[:n])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %s\n", cmd)
		os.Exit(2)
	}
}

func getHelperPath(t *testing.T) string {
	t.Helper()
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("failed to get test executable: %v", err)
	}
	abs, err := filepath.Abs(execPath)
	if err != nil {
		t.Fatalf("failed to get abs executable path: %v", err)
	}
	return filepath.Clean(abs)
}

func helperArgs(subcommand string, extra ...string) []string {
	args := []string{"-test.run=TestHelperProcess", "--", subcommand}
	return append(args, extra...)
}

func helperEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"GO_WANT_HELPER_PROCESS": "1",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func TestAcceptance1_PolicyValidationAndDenials(t *testing.T) {
	// New(Policy{AllowedExecutables: []string{"bin/tool"}}) returns *Error with Code "invalid_policy"
	_, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{"bin/tool"},
	})
	if err == nil {
		t.Fatal("expected error for non-absolute executable path")
	}
	var execErr *agentexecsandbox.Error
	if !errors.As(err, &execErr) || execErr.Code != "invalid_policy" {
		t.Fatalf("expected invalid_policy error, got %v", err)
	}

	// Test other invalid policy attributes
	_, err = agentexecsandbox.New(agentexecsandbox.Policy{Timeout: -1})
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
	_, err = agentexecsandbox.New(agentexecsandbox.Policy{MaxOutputBytes: -1})
	if err == nil {
		t.Fatal("expected error for negative max output")
	}
	_, err = agentexecsandbox.New(agentexecsandbox.Policy{MaxConcurrent: -1})
	if err == nil {
		t.Fatal("expected error for negative max concurrent")
	}
	_, err = agentexecsandbox.New(agentexecsandbox.Policy{WorkDir: "relative/path"})
	if err == nil {
		t.Fatal("expected error for non-absolute workdir")
	}

	// Run with Path absolute but not in AllowedExecutables returns Code "not_allowed"
	// without starting process (OnEvent receives exactly one Event with Kind "denied" and no "started")
	helpPath := getHelperPath(t)
	var events []agentexecsandbox.Event
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		OnEvent: func(ev agentexecsandbox.Event) {
			events = append(events, ev)
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	unallowedAbs := filepath.Clean(filepath.Join(os.TempDir(), "not_in_allow_list"))
	_, runErr := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: unallowedAbs,
	})
	if runErr == nil {
		t.Fatal("expected error for unlisted executable")
	}
	if !errors.As(runErr, &execErr) || execErr.Code != "not_allowed" {
		t.Fatalf("expected not_allowed code, got %v", runErr)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event, got %d", len(events))
	}
	if events[0].Kind != "denied" || events[0].Code != "not_allowed" {
		t.Fatalf("expected denied event with not_allowed code, got %+v", events[0])
	}

	// Test non-absolute path in Request
	_, relErr := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: "relative_helper",
	})
	if !errors.As(relErr, &execErr) || execErr.Code != "not_allowed" {
		t.Fatalf("expected not_allowed code for relative path, got %v", relErr)
	}
}

func TestAcceptance2_OutputTruncation(t *testing.T) {
	helpPath := getHelperPath(t)
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS"},
		MaxOutputBytes:     4096,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	res, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("write_bytes"),
		Env:  helperEnv(nil),
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(res.Stdout) != 4096 {
		t.Fatalf("expected 4096 bytes stdout, got %d", len(res.Stdout))
	}
	if !res.StdoutTruncated {
		t.Fatal("expected StdoutTruncated == true")
	}
	if res.StderrTruncated {
		t.Fatal("expected StderrTruncated == false")
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected ExitCode == 0, got %d", res.ExitCode)
	}
}

func TestAcceptance3_Timeout(t *testing.T) {
	helpPath := getHelperPath(t)
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS"},
		Timeout:            20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	start := time.Now()
	res, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("sleep_forever"),
		Env:  helperEnv(nil),
	})
	elapsed := time.Since(start)

	if elapsed >= 1*time.Second {
		t.Fatalf("expected run to return under 1s, took %v", elapsed)
	}
	if err != nil {
		t.Fatalf("expected err == nil on timeout, got %v", err)
	}
	if !res.TimedOut {
		t.Fatal("expected res.TimedOut == true")
	}
	if res.ExitCode != -1 {
		t.Fatalf("expected res.ExitCode == -1, got %d", res.ExitCode)
	}
}

func TestAcceptance4_EnvironmentIsolation(t *testing.T) {
	helpPath := getHelperPath(t)
	t.Setenv("SECRET_TOKEN", "s3cr3t")

	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS", "FOO"},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Helper printing SECRET_TOKEN writes ""
	res, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("print_env", "SECRET_TOKEN"),
		Env:  helperEnv(nil),
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if string(res.Stdout) != "" {
		t.Fatalf("expected empty stdout, got %q", string(res.Stdout))
	}

	// AllowedEnv []string{"FOO"} and Request.Env map[string]string{"FOO": "bar"} prints "bar"
	resFoo, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("print_env", "FOO"),
		Env:  helperEnv(map[string]string{"FOO": "bar"}),
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if string(resFoo.Stdout) != "bar" {
		t.Fatalf("expected 'bar', got %q", string(resFoo.Stdout))
	}

	// Request.Env map[string]string{"PATH2": "x"} returns Code "env_not_allowed"
	_, err = ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("print_env", "PATH2"),
		Env:  helperEnv(map[string]string{"PATH2": "x"}),
	})
	if err == nil {
		t.Fatal("expected error for unallowed env var")
	}
	var execErr *agentexecsandbox.Error
	if !errors.As(err, &execErr) || execErr.Code != "env_not_allowed" {
		t.Fatalf("expected env_not_allowed code, got %v", err)
	}
}

func TestAcceptance5_ExitCodeAndStartFailed(t *testing.T) {
	helpPath := getHelperPath(t)
	nonExistentPath := filepath.Clean(filepath.Join(os.TempDir(), "agentexecsandbox_nonexistent_binary"))
	_ = os.Remove(nonExistentPath)

	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath, nonExistentPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS"},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// A helper exiting with os.Exit(3) gives res.ExitCode == 3 and err == nil
	res, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("exit_code"),
		Env:  helperEnv(nil),
	})
	if err != nil {
		t.Fatalf("expected err == nil, got %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("expected res.ExitCode == 3, got %d", res.ExitCode)
	}

	// An allow-listed Path that does not exist on disk returns Code "start_failed"
	_, err = ex.Run(context.Background(), agentexecsandbox.Request{
		Path: nonExistentPath,
	})
	if err == nil {
		t.Fatal("expected error for non-existent binary")
	}
	var execErr *agentexecsandbox.Error
	if !errors.As(err, &execErr) || execErr.Code != "start_failed" {
		t.Fatalf("expected start_failed code, got %v", err)
	}
}

func TestAcceptance6_ConcurrencyLimit(t *testing.T) {
	helpPath := getHelperPath(t)
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS"},
		MaxConcurrent:      1,
		Timeout:            5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	started := make(chan struct{})
	var runErr1, runErr2 error
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		// Start command that runs for 500ms
		close(started)
		_, runErr1 = ex.Run(context.Background(), agentexecsandbox.Request{
			Path: helpPath,
			Args: helperArgs("sleep_forever"),
			Env:  helperEnv(nil),
		})
	}()

	<-started
	// Small delay to allow the first process to enter Run and acquire semaphore
	time.Sleep(25 * time.Millisecond)

	_, runErr2 = ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("exit_code"),
		Env:  helperEnv(nil),
	})

	if runErr2 == nil {
		t.Fatal("expected second Run to fail with busy")
	}
	var execErr *agentexecsandbox.Error
	if !errors.As(runErr2, &execErr) || execErr.Code != "busy" {
		t.Fatalf("expected busy code, got %v", runErr2)
	}

	wg.Wait()
	if runErr1 != nil {
		t.Fatalf("first run error: %v", runErr1)
	}
}

func TestAcceptance7_RequestLimits(t *testing.T) {
	helpPath := getHelperPath(t)
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS"},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	var execErr *agentexecsandbox.Error

	// 1. 257 args returns invalid_request
	args257 := make([]string, 257)
	for i := range args257 {
		args257[i] = "a"
	}
	_, err = ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: args257,
	})
	if !errors.As(err, &execErr) || execErr.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for 257 args, got %v", err)
	}

	// 2. 256 args accepted
	args256 := make([]string, 256)
	args256[0] = "-test.run=TestHelperProcess"
	args256[1] = "--"
	args256[2] = "echo_sentinel"
	for i := 3; i < 256; i++ {
		args256[i] = ""
	}
	res, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: args256,
		Env:  helperEnv(nil),
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("256 args should be accepted, err=%v exit=%d", err, res.ExitCode)
	}

	// 3. more than 65536 total arg bytes returns invalid_request
	largeArg := strings.Repeat("b", 65537)
	_, err = ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: []string{largeArg},
	})
	if !errors.As(err, &execErr) || execErr.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for arg bytes > 65536, got %v", err)
	}

	// 4. 1048577 bytes of Stdin returns invalid_request
	stdin1048577 := make([]byte, 1048577)
	_, err = ex.Run(context.Background(), agentexecsandbox.Request{
		Path:  helpPath,
		Stdin: stdin1048577,
	})
	if !errors.As(err, &execErr) || execErr.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for stdin 1048577 bytes, got %v", err)
	}

	// 5. exactly 1048576 bytes of Stdin are accepted
	stdin1048576 := make([]byte, 1048576)
	res, err = ex.Run(context.Background(), agentexecsandbox.Request{
		Path:  helpPath,
		Args:  helperArgs("echo_sentinel"),
		Env:   helperEnv(nil),
		Stdin: stdin1048576,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("1048576 bytes stdin should be accepted, err=%v exit=%d", err, res.ExitCode)
	}
}

func TestAcceptance8_EventsAndSentinelLeakCheck(t *testing.T) {
	helpPath := getHelperPath(t)
	sentinel := "SENTINEL-7f3a"

	var events []agentexecsandbox.Event
	var mu sync.Mutex
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS", "MY_PARAM"},
		OnEvent: func(ev agentexecsandbox.Event) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	res, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("echo_sentinel", sentinel),
		Env:  helperEnv(map[string]string{"MY_PARAM": sentinel}),
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != "started" {
		t.Fatalf("expected first event kind 'started', got %q", events[0].Kind)
	}
	if events[1].Kind != "finished" {
		t.Fatalf("expected second event kind 'finished', got %q", events[1].Kind)
	}

	for _, ev := range events {
		if strings.Contains(ev.Kind, sentinel) ||
			strings.Contains(ev.Path, sentinel) ||
			strings.Contains(ev.Code, sentinel) {
			t.Fatalf("event leaked sentinel %q: %+v", sentinel, ev)
		}
	}
}

func TestCallerCancellation(t *testing.T) {
	helpPath := getHelperPath(t)
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_HELPER_PROCESS"},
		Timeout:            10 * time.Second,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, runErr := ex.Run(ctx, agentexecsandbox.Request{
		Path: helpPath,
		Args: helperArgs("sleep_forever"),
		Env:  helperEnv(nil),
	})
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", runErr)
	}
}

func TestErrorMessageFormatting(t *testing.T) {
	err := &agentexecsandbox.Error{Code: "test_code", Msg: "something went wrong"}
	if got, want := err.Error(), "agentexecsandbox: test_code: something went wrong"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

//go:build unix

package agentexecsandbox_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Trysac/u126-agent-exec-sandbox"
)

// TestGrandchildProcessKill verifies that when a process spawns a grandchild,
// timing out kills the whole process group rather than leaving grandchildren running.
func TestGrandchildProcessKill(t *testing.T) {
	// If invoked as the grandchild sleeper
	if os.Getenv("GO_WANT_GRANDCHILD_PROCESS") == "1" {
		// Sleep for 30s
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}

	// If invoked as the child process that spawns grandchild
	if os.Getenv("GO_WANT_CHILD_SPAWNER") == "1" {
		cmd := exec.Command(os.Args[0], "-test.run=TestGrandchildProcessKill")
		cmd.Env = append(os.Environ(), "GO_WANT_GRANDCHILD_PROCESS=1")
		if err := cmd.Start(); err != nil {
			os.Exit(1)
		}
		// Child stays alive waiting or sleeping
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}

	helpPath := getHelperPath(t)
	ex, err := agentexecsandbox.New(agentexecsandbox.Policy{
		AllowedExecutables: []string{helpPath},
		AllowedEnv:         []string{"GO_WANT_CHILD_SPAWNER"},
		Timeout:            50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	start := time.Now()
	res, err := ex.Run(context.Background(), agentexecsandbox.Request{
		Path: helpPath,
		Args: []string{"-test.run=TestGrandchildProcessKill"},
		Env: map[string]string{
			"GO_WANT_CHILD_SPAWNER": "1",
		},
	})
	elapsed := time.Since(start)

	if elapsed >= 2*time.Second {
		t.Fatalf("expected command to terminate quickly, took %v", elapsed)
	}
	if err != nil {
		t.Fatalf("expected nil err on timeout, got %v", err)
	}
	if !res.TimedOut {
		t.Fatal("expected res.TimedOut to be true")
	}
	if res.ExitCode != -1 {
		t.Fatalf("expected res.ExitCode -1, got %d", res.ExitCode)
	}
}

package main

import (
	"testing"
)

func TestRunCmd_Flags(t *testing.T) {
	cmd := runCmd()

	flags := []struct {
		name      string
		shorthand string
	}{
		{"interactive", "i"},
		{"existing-pod", "e"},
		{"container", "c"},
		{"cpu", ""},
		{"ram", ""},
		{"verbose", ""},
	}

	for _, f := range flags {
		flag := cmd.Flags().Lookup(f.name)
		if flag == nil {
			t.Errorf("expected flag --%s to be registered", f.name)
			continue
		}
		if f.shorthand != "" && flag.Shorthand != f.shorthand {
			t.Errorf("flag --%s: expected shorthand %q, got %q", f.name, f.shorthand, flag.Shorthand)
		}
	}
}

func TestRunCmd_HasCleanupSubcommand(t *testing.T) {
	cmd := runCmd()

	var found bool
	for _, sub := range cmd.Commands() {
		if sub.Name() == "cleanup" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cleanup to be a subcommand of run")
	}
}

func TestAppsCmd_HasRunSubcommand(t *testing.T) {
	cmd := appsCmd()

	var found bool
	for _, sub := range cmd.Commands() {
		if sub.Name() == "run" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected run to be a subcommand of apps")
	}
}

func TestCleanupCmd_ExactArgs(t *testing.T) {
	cmd := cleanupCmd()

	// Verify that the command requires exactly 1 argument
	err := cmd.Args(cmd, []string{})
	if err == nil {
		t.Error("expected error when no args provided to cleanup")
	}

	err = cmd.Args(cmd, []string{"app-id"})
	if err != nil {
		t.Errorf("expected no error with 1 arg, got: %v", err)
	}

	err = cmd.Args(cmd, []string{"app-id", "extra"})
	if err == nil {
		t.Error("expected error when too many args provided to cleanup")
	}
}

func TestRunCmd_RequiresAppID(t *testing.T) {
	cmd := runCmd()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when no app-id provided")
	}
}

func TestRunCmd_NonInteractiveRequiresCommand(t *testing.T) {
	cmd := runCmd()
	// Provide app-id but no command (no -- separator)
	cmd.SetArgs([]string{"my-app"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error when no command provided in non-interactive mode")
	}
}

func TestRunCmd_DefaultFlagValues(t *testing.T) {
	cmd := runCmd()

	// interactive defaults to false
	interactive, err := cmd.Flags().GetBool("interactive")
	if err != nil {
		t.Fatalf("failed to get interactive flag: %v", err)
	}
	if interactive {
		t.Error("expected interactive default to be false")
	}

	// cpu defaults to 0
	cpu, err := cmd.Flags().GetInt("cpu")
	if err != nil {
		t.Fatalf("failed to get cpu flag: %v", err)
	}
	if cpu != 0 {
		t.Errorf("expected cpu default to be 0, got %d", cpu)
	}

	// ram defaults to 0
	ram, err := cmd.Flags().GetInt("ram")
	if err != nil {
		t.Fatalf("failed to get ram flag: %v", err)
	}
	if ram != 0 {
		t.Errorf("expected ram default to be 0, got %d", ram)
	}
}

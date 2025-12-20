package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestCLIHelp verifies that the CLI help command works
func TestCLIHelp(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("CLI help failed: %v\nstderr: %s", err, stderr.String())
	}

	output := stdout.String()
	expectedPhrases := []string{
		"NEVR Agent",
		"EchoVR",
		"stream",
		"convert",
		"replay",
		"serve",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(output, phrase) {
			t.Errorf("Expected help output to contain %q, but it didn't.\nOutput: %s", phrase, output)
		}
	}
}

// TestCLIVersion verifies that the version command works
func TestCLIVersion(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("CLI version failed: %v\nstderr: %s", err, stderr.String())
	}

	output := stdout.String()
	// Version should contain "agent" and some version string
	if !strings.Contains(output, "agent") {
		t.Errorf("Expected version output to contain 'agent', got: %s", output)
	}
}

// TestCLISubcommandHelp verifies that subcommand help works
func TestCLISubcommandHelp(t *testing.T) {
	subcommands := []string{"stream", "convert", "replay", "serve"}

	for _, subcmd := range subcommands {
		t.Run(subcmd, func(t *testing.T) {
			cmd := exec.Command("go", "run", ".", subcmd, "--help")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				t.Fatalf("CLI %s help failed: %v\nstderr: %s", subcmd, err, stderr.String())
			}

			output := stdout.String()
			if len(output) == 0 {
				t.Errorf("Expected non-empty help output for %s subcommand", subcmd)
			}
		})
	}
}

// TestCLIInvalidSubcommand verifies that invalid subcommands fail gracefully
func TestCLIInvalidSubcommand(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "invalid-subcommand")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected error for invalid subcommand, but got none")
	}

	// Should contain some error message
	errOutput := stderr.String()
	if !strings.Contains(errOutput, "unknown command") {
		t.Errorf("Expected error message to contain 'unknown command', got: %s", errOutput)
	}
}

// TestCLIStreamRequiresTarget verifies that stream command requires a target
func TestCLIStreamRequiresTarget(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "stream")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected error when running stream without target, but got none")
	}
}

// TestCLIConvertRequiresInput verifies that convert command requires input file
func TestCLIConvertRequiresInput(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "convert")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected error when running convert without input, but got none")
	}
}

// TestCLIReplayRequiresFiles verifies that replay command requires files
func TestCLIReplayRequiresFiles(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "replay")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatal("Expected error when running replay without files, but got none")
	}
}

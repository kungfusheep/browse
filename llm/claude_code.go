package llm

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/google/uuid"
)

// ClaudeCode implements Provider by shelling out to the claude CLI.
// This is ideal for users running browse inside Claude Code - no API key needed!
type ClaudeCode struct {
	cliPath string
}

// NewClaudeCode creates a new Claude Code provider.
func NewClaudeCode() *ClaudeCode {
	return &ClaudeCode{}
}

// Name returns the provider name.
func (c *ClaudeCode) Name() string {
	return "claude-code"
}

// Available checks if the claude CLI is installed and accessible.
func (c *ClaudeCode) Available() bool {
	path, err := exec.LookPath("claude")
	if err != nil {
		return false
	}
	c.cliPath = path
	return true
}

// Complete sends a prompt to claude CLI and returns the response.
func (c *ClaudeCode) Complete(ctx context.Context, prompt string) (string, error) {
	return c.run(ctx, "", prompt)
}

// CompleteWithSystem sends a prompt with system message to claude CLI.
func (c *ClaudeCode) CompleteWithSystem(ctx context.Context, system, prompt string) (string, error) {
	return c.run(ctx, system, prompt)
}

// CompleteConversation sends a multi-turn conversation to claude CLI using session IDs.
// Each user message is sent with the same session ID to maintain context.
func (c *ClaudeCode) CompleteConversation(ctx context.Context, system string, messages []Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// Retry with fresh session ID if we hit a collision
	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err := c.runConversation(ctx, system, messages)
		if err == nil {
			return result, nil
		}
		// Check if it's a session collision error - if so, retry with new session
		if cliErr, ok := err.(*CLIError); ok {
			if strings.Contains(cliErr.Stderr, "already in use") {
				continue // Retry with new session ID
			}
		}
		return "", err
	}
	return "", &CLIError{Err: ErrSessionCollision, Stderr: "session ID collision after max retries"}
}

func (c *ClaudeCode) runConversation(ctx context.Context, system string, messages []Message) (string, error) {
	// Generate a session ID for this conversation
	sessionID := uuid.New().String()

	var lastResponse string
	isFirst := true

	for _, msg := range messages {
		if msg.Role != "user" {
			continue // Skip assistant messages - they're already in the session
		}

		args := []string{
			"--print",
			"--session-id", sessionID,
		}

		// Add system prompt only on first message
		if isFirst && system != "" {
			args = append(args, "--system-prompt", system)
		}
		isFirst = false

		args = append(args, msg.Content)

		cmd := exec.CommandContext(ctx, c.cliPath, args...)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			if stderr.Len() > 0 {
				return "", &CLIError{Err: err, Stderr: stderr.String()}
			}
			return "", err
		}

		lastResponse = strings.TrimSpace(stdout.String())
	}

	return lastResponse, nil
}

func (c *ClaudeCode) run(ctx context.Context, system, prompt string) (string, error) {
	args := []string{
		"--print", // Output response and exit (non-interactive mode)
	}

	// Add system prompt if provided
	if system != "" {
		args = append(args, "--system-prompt", system)
	}

	// Add the user prompt
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, c.cliPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Include stderr in error for debugging
		if stderr.Len() > 0 {
			return "", &CLIError{
				Err:    err,
				Stderr: stderr.String(),
			}
		}
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// CLIError wraps CLI execution errors with stderr output.
type CLIError struct {
	Err    error
	Stderr string
}

func (e *CLIError) Error() string {
	if e.Stderr != "" {
		return e.Err.Error() + ": " + e.Stderr
	}
	return e.Err.Error()
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

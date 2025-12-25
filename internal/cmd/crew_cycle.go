package cmd

import (
	"fmt"
	"os/exec"
	"sort"

	"github.com/spf13/cobra"
)

// cycleCrewSession switches to the next or previous crew session across ALL rigs.
// direction: 1 for next, -1 for previous
func cycleCrewSession(direction int) error {
	// Get current session (uses existing function from handoff.go)
	currentSession, err := getCurrentTmuxSession()
	if err != nil {
		return fmt.Errorf("not in a tmux session: %w", err)
	}
	if currentSession == "" {
		return fmt.Errorf("not in a tmux session")
	}

	// Verify we're in a crew session
	_, _, ok := parseCrewSessionName(currentSession)
	if !ok {
		return fmt.Errorf("not in a crew session (expected gt-<rig>-crew-<name>)")
	}

	// Find all crew sessions across all rigs
	sessions, err := findAllCrewSessions()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		return fmt.Errorf("no crew sessions found")
	}

	// Sort for consistent ordering
	sort.Strings(sessions)

	// Find current position
	currentIdx := -1
	for i, s := range sessions {
		if s == currentSession {
			currentIdx = i
			break
		}
	}

	if currentIdx == -1 {
		// Current session not in list (shouldn't happen)
		return fmt.Errorf("current session not found in crew list")
	}

	// Calculate target index (with wrapping)
	targetIdx := (currentIdx + direction + len(sessions)) % len(sessions)

	if targetIdx == currentIdx {
		// Only one session, nothing to switch to
		return nil
	}

	targetSession := sessions[targetIdx]

	// Switch to target session
	cmd := exec.Command("tmux", "switch-client", "-t", targetSession)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("switching to %s: %w", targetSession, err)
	}

	return nil
}

func runCrewNext(cmd *cobra.Command, args []string) error {
	return cycleCrewSession(1)
}

func runCrewPrev(cmd *cobra.Command, args []string) error {
	return cycleCrewSession(-1)
}

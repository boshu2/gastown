package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	cleanupDryRun       bool
	cleanupGC           bool
	cleanupOnlyPolecats bool
	cleanupOnlyConvoys  bool
)

var cleanupCmd = &cobra.Command{
	Use:     "cleanup",
	GroupID: GroupWorkspace,
	Short:   "Clean up done polecats and completed convoys",
	Long: `One-stop cleanup for Gas Town.

Finds and nukes "done" polecats across all rigs and auto-closes completed convoys.
This is the canonical cleanup command - use it regularly to keep your workspace tidy.

Unlike 'gt polecat stale', this command specifically targets polecats in the "done"
state (zombies with potentially running sessions) and cleans them regardless of
session state.

Examples:
  gt cleanup              # Nuke all done polecats, close completed convoys
  gt cleanup --dry-run    # Preview what would be cleaned up
  gt cleanup --gc         # Also gc stale branches after cleanup
  gt cleanup --polecats   # Only clean polecats (skip convoys)
  gt cleanup --convoys    # Only close convoys (skip polecats)`,
	RunE: runCleanup,
}

func init() {
	cleanupCmd.Flags().BoolVar(&cleanupDryRun, "dry-run", false, "Preview what would be cleaned up")
	cleanupCmd.Flags().BoolVar(&cleanupGC, "gc", false, "Also gc stale branches after cleanup")
	cleanupCmd.Flags().BoolVar(&cleanupOnlyPolecats, "polecats", false, "Only clean polecats (skip convoys)")
	cleanupCmd.Flags().BoolVar(&cleanupOnlyConvoys, "convoys", false, "Only close convoys (skip polecats)")

	rootCmd.AddCommand(cleanupCmd)
}

func runCleanup(cmd *cobra.Command, args []string) error {
	// Default: clean both polecats and convoys
	cleanBoth := !cleanupOnlyPolecats && !cleanupOnlyConvoys

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// Load rigs config
	rigsConfigPath := filepath.Join(townRoot, "mayor", "rigs.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		rigsConfig = &config.RigsConfig{Rigs: make(map[string]config.RigEntry)}
	}

	// Discover all rigs
	g := git.NewGit(townRoot)
	rigMgr := rig.NewManager(townRoot, rigsConfig, g)
	rigs, err := rigMgr.DiscoverRigs()
	if err != nil {
		return fmt.Errorf("discovering rigs: %w", err)
	}

	if cleanupDryRun {
		fmt.Printf("%s Cleanup preview (--dry-run)\n\n", style.Bold.Render("🧹"))
	} else {
		fmt.Printf("%s Gas Town cleanup\n\n", style.Bold.Render("🧹"))
	}

	var totalPolecatsNuked int
	var totalConvoysClosed int
	var totalBranchesGCed int

	// Clean polecats
	if cleanBoth || cleanupOnlyPolecats {
		nuked, err := cleanupDonePolecats(rigs, cleanupDryRun)
		if err != nil {
			style.PrintWarning("polecat cleanup had errors: %v", err)
		}
		totalPolecatsNuked = nuked
	}

	// Close convoys
	if cleanBoth || cleanupOnlyConvoys {
		townBeads := filepath.Join(townRoot, ".beads")
		closed, err := cleanupCompletedConvoys(townBeads, cleanupDryRun)
		if err != nil {
			style.PrintWarning("convoy cleanup had errors: %v", err)
		}
		totalConvoysClosed = closed
	}

	// GC branches if requested
	if cleanupGC && (cleanBoth || cleanupOnlyPolecats) {
		gcCount, err := cleanupStaleBranches(rigs, cleanupDryRun)
		if err != nil {
			style.PrintWarning("branch gc had errors: %v", err)
		}
		totalBranchesGCed = gcCount
	}

	// Summary
	fmt.Println()
	if cleanupDryRun {
		fmt.Printf("%s Dry run complete. Would clean:\n", style.Bold.Render("📋"))
	} else {
		fmt.Printf("%s Cleanup complete:\n", style.Bold.Render("✓"))
	}

	if cleanBoth || cleanupOnlyPolecats {
		if totalPolecatsNuked > 0 {
			fmt.Printf("  - %d polecat(s) nuked\n", totalPolecatsNuked)
		} else {
			fmt.Printf("  - No done polecats found\n")
		}
	}

	if cleanBoth || cleanupOnlyConvoys {
		if totalConvoysClosed > 0 {
			fmt.Printf("  - %d convoy(s) closed\n", totalConvoysClosed)
		} else {
			fmt.Printf("  - No completed convoys found\n")
		}
	}

	if cleanupGC {
		if totalBranchesGCed > 0 {
			fmt.Printf("  - %d branch(es) gc'd\n", totalBranchesGCed)
		} else {
			fmt.Printf("  - No stale branches found\n")
		}
	}

	return nil
}

// cleanupDonePolecats finds and nukes all polecats in "done" state.
func cleanupDonePolecats(rigs []*rig.Rig, dryRun bool) (int, error) {
	t := tmux.NewTmux()
	var totalNuked int

	for _, r := range rigs {
		g := git.NewGit(r.Path)
		mgr := polecat.NewManager(r, g)

		polecats, err := mgr.List()
		if err != nil {
			style.PrintWarning("error listing polecats in %s: %v", r.Name, err)
			continue
		}

		// Find "done" polecats
		var donePolecats []*polecat.Polecat
		for _, p := range polecats {
			if p.State == polecat.StateDone {
				donePolecats = append(donePolecats, p)
			}
		}

		if len(donePolecats) == 0 {
			continue
		}

		fmt.Printf("%s %s: %d done polecat(s)\n", style.Bold.Render("🔍"), r.Name, len(donePolecats))

		for _, p := range donePolecats {
			if dryRun {
				fmt.Printf("  Would nuke: %s/%s\n", r.Name, p.Name)
				totalNuked++
				continue
			}

			fmt.Printf("  Nuking %s/%s...", r.Name, p.Name)

			// Kill session if running
			sessMgr := polecat.NewSessionManager(t, r)
			running, _ := sessMgr.IsRunning(p.Name)
			if running {
				_ = sessMgr.Stop(p.Name, true) // Force kill
			}

			// Remove the polecat (force=true since we know it's done)
			if err := mgr.Remove(p.Name, true); err != nil {
				fmt.Printf(" %s (%v)\n", style.Error.Render("failed"), err)
				continue
			}

			// Close the agent bead via bd command
			agentBeadID := beads.PolecatBeadID(r.Name, p.Name)
			closeCmd := exec.Command("bd", "close", agentBeadID, "-r", "Nuked by gt cleanup")
			closeCmd.Dir = r.Path
			_ = closeCmd.Run() // Best effort, ignore errors

			fmt.Printf(" %s\n", style.Success.Render("done"))
			totalNuked++
		}
	}

	return totalNuked, nil
}

// cleanupCompletedConvoys closes convoys where all tracked issues are complete.
func cleanupCompletedConvoys(townBeads string, dryRun bool) (int, error) {
	if dryRun {
		// For dry run, just list what would be closed
		closed, err := previewCompletedConvoys(townBeads)
		if err != nil {
			return 0, err
		}
		for _, c := range closed {
			fmt.Printf("  Would close convoy: %s (%s)\n", c.ID, c.Title)
		}
		return len(closed), nil
	}

	// Use existing function from convoy.go
	closed, err := checkAndCloseCompletedConvoys(townBeads)
	if err != nil {
		return 0, err
	}

	for _, c := range closed {
		fmt.Printf("  Closed convoy: %s (%s)\n", c.ID, c.Title)
	}

	return len(closed), nil
}

// previewCompletedConvoys lists convoys that would be closed (for dry-run).
// Uses the same logic as checkAndCloseCompletedConvoys but without closing.
func previewCompletedConvoys(townBeads string) ([]struct{ ID, Title string }, error) {
	// List all open convoys via bd command
	listCmd := exec.Command("bd", "list", "--type=convoy", "--status=open", "--json")
	listCmd.Dir = townBeads
	output, err := listCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing convoys: %w", err)
	}

	var convoys []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(output, &convoys); err != nil {
		return nil, fmt.Errorf("parsing convoy list: %w", err)
	}

	var completed []struct{ ID, Title string }
	for _, convoy := range convoys {
		// Check if all tracked issues are closed
		tracked := getTrackedIssues(townBeads, convoy.ID)
		if len(tracked) == 0 {
			continue
		}

		allClosed := true
		for _, t := range tracked {
			if t.Status != "closed" && t.Status != "tombstone" {
				allClosed = false
				break
			}
		}

		if allClosed {
			completed = append(completed, struct{ ID, Title string }{convoy.ID, convoy.Title})
		}
	}

	return completed, nil
}

// cleanupStaleBranches runs gc on all rigs.
func cleanupStaleBranches(rigs []*rig.Rig, dryRun bool) (int, error) {
	var totalDeleted int

	for _, r := range rigs {
		g := git.NewGit(r.Path)
		mgr := polecat.NewManager(r, g)

		if dryRun {
			// For dry run, just count what would be deleted
			// We can't easily preview this, so skip with a note
			fmt.Printf("  Would gc branches in %s\n", r.Name)
			continue
		}

		deleted, err := mgr.CleanupStaleBranches()
		if err != nil {
			style.PrintWarning("gc failed in %s: %v", r.Name, err)
			continue
		}

		if deleted > 0 {
			fmt.Printf("  GC'd %d branch(es) in %s\n", deleted, r.Name)
			totalDeleted += deleted
		}
	}

	return totalDeleted, nil
}

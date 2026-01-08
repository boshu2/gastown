package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
)

// Cleanup command flags
var (
	cleanupDryRun       bool
	cleanupGC           bool
	cleanupPolecatsOnly bool
	cleanupConvoysOnly  bool
)

var cleanupCmd = &cobra.Command{
	Use:     "cleanup",
	GroupID: GroupWorkspace,
	Short:   "Clean up done polecats and completed convoys",
	Long: `One-stop cleanup for Gas Town.

This command handles cleanup of:
  - Done polecats: Polecats in "done" state are nuked (including zombies with running sessions)
  - Completed convoys: Convoys where all tracked issues are closed
  - Stale branches: Orphaned polecat branches (with --gc flag)

The key difference from 'gt polecat stale' is that cleanup directly checks polecat
state (done = no assigned work) rather than session/commit staleness. This catches
"zombie" polecats that have running tmux sessions but have completed their work.

Examples:
  gt cleanup              # Nuke done polecats, close convoys across all rigs
  gt cleanup --dry-run    # Preview what would happen
  gt cleanup --gc         # Also garbage collect stale branches
  gt cleanup --polecats   # Only clean up polecats (skip convoys)
  gt cleanup --convoys    # Only close convoys (skip polecats)`,
	RunE: runCleanup,
}

func init() {
	cleanupCmd.Flags().BoolVar(&cleanupDryRun, "dry-run", false, "Preview what would be cleaned without doing it")
	cleanupCmd.Flags().BoolVar(&cleanupGC, "gc", false, "Also garbage collect stale branches")
	cleanupCmd.Flags().BoolVar(&cleanupPolecatsOnly, "polecats", false, "Only clean up polecats (skip convoys)")
	cleanupCmd.Flags().BoolVar(&cleanupConvoysOnly, "convoys", false, "Only close convoys (skip polecats)")

	rootCmd.AddCommand(cleanupCmd)
}

// cleanupStats tracks cleanup results
type cleanupStats struct {
	polecatsNuked  int
	polecatsFailed int
	convoysClosed  int
	branchesGCed   int
}

func runCleanup(cmd *cobra.Command, args []string) error {
	// Get all rigs
	rigs, townRoot, err := getAllRigs()
	if err != nil {
		return err
	}

	if len(rigs) == 0 {
		fmt.Println("No rigs found.")
		return nil
	}

	stats := cleanupStats{}
	t := tmux.NewTmux()

	// Phase 1: Clean up done polecats across all rigs
	if !cleanupConvoysOnly {
		if cleanupDryRun {
			fmt.Printf("%s Previewing polecat cleanup...\n\n", style.Info.Render("ℹ"))
		} else {
			fmt.Printf("%s Cleaning up done polecats...\n\n", style.Bold.Render("🧹"))
		}

		for _, r := range rigs {
			polecatGit := git.NewGit(r.Path)
			mgr := polecat.NewManager(r, polecatGit)
			sessMgr := session.NewManager(t, r)

			polecats, err := mgr.List()
			if err != nil {
				fmt.Printf("  %s %s: failed to list polecats: %v\n", style.Warning.Render("⚠"), r.Name, err)
				continue
			}

			// Find polecats in "done" state (ready for cleanup)
			for _, p := range polecats {
				if p.State != polecat.StateDone {
					continue
				}

				// This is a zombie - it has completed work but still exists
				running, _ := sessMgr.IsRunning(p.Name)
				sessionStatus := style.Dim.Render("no session")
				if running {
					sessionStatus = style.Warning.Render("zombie session")
				}

				if cleanupDryRun {
					fmt.Printf("  Would nuke: %s/%s (%s) %s\n",
						r.Name, p.Name, style.Success.Render("done"), sessionStatus)
					stats.polecatsNuked++
				} else {
					fmt.Printf("  Nuking %s/%s (%s)...", r.Name, p.Name, sessionStatus)

					// Kill session first if running
					if running {
						if err := sessMgr.Stop(p.Name, true); err != nil {
							fmt.Printf(" %s\n", style.Warning.Render("session kill failed"))
							// Continue anyway
						}
					}

					// Nuke the polecat (force mode, nuclear option)
					if err := mgr.RemoveWithOptions(p.Name, true, true); err != nil {
						fmt.Printf(" %s (%v)\n", style.Error.Render("failed"), err)
						stats.polecatsFailed++
					} else {
						fmt.Printf(" %s\n", style.Success.Render("done"))
						stats.polecatsNuked++
					}
				}
			}
		}

		if stats.polecatsNuked == 0 && stats.polecatsFailed == 0 {
			fmt.Println("  No done polecats found.")
		}
		fmt.Println()
	}

	// Phase 2: Close completed convoys
	if !cleanupPolecatsOnly {
		townBeads, err := getTownBeadsDir()
		if err != nil {
			fmt.Printf("%s Skipping convoy check: %v\n", style.Warning.Render("⚠"), err)
		} else {
			if cleanupDryRun {
				fmt.Printf("%s Previewing convoy cleanup...\n\n", style.Info.Render("ℹ"))
				// For dry-run, we need to check what would be closed
				// Use the same logic as checkAndCloseCompletedConvoys but don't close
				fmt.Println("  (convoy dry-run not yet implemented - run without --dry-run to close)")
			} else {
				fmt.Printf("%s Closing completed convoys...\n\n", style.Bold.Render("🚚"))

				closed, err := checkAndCloseCompletedConvoys(townBeads)
				if err != nil {
					fmt.Printf("  %s convoy check failed: %v\n", style.Warning.Render("⚠"), err)
				} else if len(closed) == 0 {
					fmt.Println("  No convoys ready to close.")
				} else {
					for _, c := range closed {
						fmt.Printf("  %s Closed: %s - %s\n", style.Success.Render("✓"), c.ID, c.Title)
					}
					stats.convoysClosed = len(closed)
				}
			}
			fmt.Println()
		}
	}

	// Phase 3: Garbage collect stale branches (optional)
	if cleanupGC {
		if cleanupDryRun {
			fmt.Printf("%s Previewing branch garbage collection...\n\n", style.Info.Render("ℹ"))
		} else {
			fmt.Printf("%s Garbage collecting stale branches...\n\n", style.Bold.Render("🗑"))
		}

		for _, r := range rigs {
			polecatGit := git.NewGit(r.Path)
			mgr := polecat.NewManager(r, polecatGit)

			if cleanupDryRun {
				// Show what would be cleaned - use the same logic as polecat gc --dry-run
				fmt.Printf("  %s: (use 'gt polecat gc %s --dry-run' for details)\n", r.Name, r.Name)
			} else {
				deleted, err := mgr.CleanupStaleBranches()
				if err != nil {
					fmt.Printf("  %s %s: gc failed: %v\n", style.Warning.Render("⚠"), r.Name, err)
				} else if deleted == 0 {
					fmt.Printf("  %s: no stale branches\n", r.Name)
				} else {
					fmt.Printf("  %s: %s deleted %d stale branch(es)\n", r.Name, style.Success.Render("✓"), deleted)
					stats.branchesGCed += deleted
				}
			}
		}
		fmt.Println()
	}

	// Summary
	if cleanupDryRun {
		fmt.Printf("%s Dry run complete.\n", style.Info.Render("ℹ"))
		if !cleanupConvoysOnly && stats.polecatsNuked > 0 {
			fmt.Printf("  Would nuke: %d polecat(s)\n", stats.polecatsNuked)
		}
		fmt.Println("\nRun without --dry-run to perform cleanup.")
	} else {
		fmt.Printf("%s Cleanup complete.\n", style.SuccessPrefix)
		if !cleanupConvoysOnly {
			fmt.Printf("  Polecats nuked: %d\n", stats.polecatsNuked)
			if stats.polecatsFailed > 0 {
				fmt.Printf("  Polecats failed: %d\n", stats.polecatsFailed)
			}
		}
		if !cleanupPolecatsOnly {
			fmt.Printf("  Convoys closed: %d\n", stats.convoysClosed)
		}
		if cleanupGC {
			fmt.Printf("  Branches GCed: %d\n", stats.branchesGCed)
		}
	}

	// Return error if we had failures
	if stats.polecatsFailed > 0 {
		return fmt.Errorf("%d polecat cleanup(s) failed", stats.polecatsFailed)
	}

	_ = townRoot // Silence unused variable warning
	return nil
}

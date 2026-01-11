package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Shard command flags
var (
	shardCreateRig       string
	shardCreateNamespace string
	shardCreateModel     string
	shardListJSON        bool
	shardListRig         string
	shardListAll         bool
	shardStatusJSON      bool
	shardAbortForce      bool
)

var shardCmd = &cobra.Command{
	Use:     "shard",
	GroupID: GroupAgents,
	Short:   "Manage K8s-native agent execution (Daedalus-Cyclopes)",
	RunE:    requireSubcommand,
	Long: `Manage shards - K8s-native agent execution units.

A shard is a Kubernetes Job that runs an AI agent to complete a task.
This enables cloud-native scaling beyond local tmux-based polecats.

WHAT IS A SHARD:
  - A Kubernetes Job running an agent container
  - Created with an objective (what to accomplish)
  - Runs in a specified rig's namespace
  - Tracked via beads for status and lifecycle

COMPARISON TO POLECATS:
  - Polecats: local tmux sessions, immediate, low latency
  - Shards: K8s Jobs, scalable, cloud-native, higher latency

COMMANDS:
  create    Create a new shard with an objective
  list      List shards in a rig
  status    Show detailed shard status
  abort     Abort a running shard`,
}

// shardCreateObjective holds the objective flag value
var shardCreateObjective string

var shardCreateCmd = &cobra.Command{
	Use:   "create --objective <objective> --rig <rig> [--namespace <ns>]",
	Short: "Create a new shard",
	Long: `Create a new shard to execute an objective via Kubernetes.

The shard creates a K8s Job that runs an agent container. The agent
will work on the objective until completion, then report results.

Examples:
  gt shard create --objective "implement login feature" --rig greenplace
  gt shard create --objective "fix bug in auth.go" --rig greenplace --namespace prod
  gt shard create --objective "review PR #123" --rig greenplace --model haiku`,
	RunE: runShardCreate,
}

var shardListCmd = &cobra.Command{
	Use:   "list [--rig <rig>]",
	Short: "List shards",
	Long: `List shards, optionally filtered by rig.

Examples:
  gt shard list                    # All shards
  gt shard list --rig greenplace   # Shards in specific rig
  gt shard list --all              # Include completed shards
  gt shard list --json`,
	RunE: runShardList,
}

var shardStatusCmd = &cobra.Command{
	Use:   "status <shard-id>",
	Short: "Show shard status",
	Long: `Show detailed status for a shard.

Displays shard metadata, K8s Job status, and execution progress.

Examples:
  gt shard status hq-sh-abc
  gt shard status hq-sh-abc --json`,
	Args: cobra.ExactArgs(1),
	RunE: runShardStatus,
}

var shardAbortCmd = &cobra.Command{
	Use:   "abort <shard-id>",
	Short: "Abort a running shard",
	Long: `Abort a running shard, terminating its K8s Job.

This will:
  1. Delete the K8s Job (terminating the pod)
  2. Mark the shard bead as aborted
  3. Release any resources

Use --force to skip confirmation.

Examples:
  gt shard abort hq-sh-abc
  gt shard abort hq-sh-abc --force`,
	Args: cobra.ExactArgs(1),
	RunE: runShardAbort,
}

func init() {
	// Create flags
	shardCreateCmd.Flags().StringVar(&shardCreateObjective, "objective", "", "Objective for the shard (required)")
	shardCreateCmd.Flags().StringVar(&shardCreateRig, "rig", "", "Target rig for the shard (required)")
	shardCreateCmd.Flags().StringVar(&shardCreateNamespace, "namespace", "", "K8s namespace (defaults to rig's namespace)")
	shardCreateCmd.Flags().StringVar(&shardCreateModel, "model", "sonnet", "Model to use (haiku, sonnet, opus)")
	shardCreateCmd.MarkFlagRequired("objective")
	shardCreateCmd.MarkFlagRequired("rig")

	// List flags
	shardListCmd.Flags().BoolVar(&shardListJSON, "json", false, "Output as JSON")
	shardListCmd.Flags().StringVar(&shardListRig, "rig", "", "Filter by rig")
	shardListCmd.Flags().BoolVar(&shardListAll, "all", false, "Include completed shards")

	// Status flags
	shardStatusCmd.Flags().BoolVar(&shardStatusJSON, "json", false, "Output as JSON")

	// Abort flags
	shardAbortCmd.Flags().BoolVarP(&shardAbortForce, "force", "f", false, "Skip confirmation")

	// Add subcommands
	shardCmd.AddCommand(shardCreateCmd)
	shardCmd.AddCommand(shardListCmd)
	shardCmd.AddCommand(shardStatusCmd)
	shardCmd.AddCommand(shardAbortCmd)

	rootCmd.AddCommand(shardCmd)
}

// ShardInfo represents a shard's state.
type ShardInfo struct {
	ID          string    `json:"id"`
	Objective   string    `json:"objective"`
	Rig         string    `json:"rig"`
	Namespace   string    `json:"namespace"`
	Model       string    `json:"model"`
	Status      string    `json:"status"` // pending, running, completed, failed, aborted
	JobName     string    `json:"job_name,omitempty"`
	PodName     string    `json:"pod_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Result      string    `json:"result,omitempty"`
}

func runShardCreate(cmd *cobra.Command, args []string) error {
	objective := shardCreateObjective

	// Validate rig exists
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	rigPath := filepath.Join(townRoot, shardCreateRig)
	if _, err := os.Stat(rigPath); os.IsNotExist(err) {
		return fmt.Errorf("rig '%s' not found", shardCreateRig)
	}

	// Default namespace to rig name if not specified
	namespace := shardCreateNamespace
	if namespace == "" {
		namespace = shardCreateRig
	}

	// Generate shard ID
	shardID := fmt.Sprintf("hq-sh-%s", generateShortID())

	// Create shard bead in town beads
	townBeads := filepath.Join(townRoot, ".beads")

	description := fmt.Sprintf("Shard for K8s execution\nRig: %s\nNamespace: %s\nModel: %s\nObjective: %s",
		shardCreateRig, namespace, shardCreateModel, objective)

	createArgs := []string{
		"create",
		"--type=shard",
		"--id=" + shardID,
		"--title=" + truncateString(objective, 50),
		"--description=" + description,
		"--json",
	}

	createCmd := exec.Command("bd", createArgs...)
	createCmd.Dir = townBeads
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	createCmd.Stdout = &stdout
	createCmd.Stderr = &stderr

	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("creating shard bead: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	// TODO: Actually create K8s Job via Daedalus API
	// For now, we just create the tracking bead and print instructions

	fmt.Printf("%s Created shard 🔷 %s\n\n", style.Bold.Render("✓"), shardID)
	fmt.Printf("  Objective:  %s\n", objective)
	fmt.Printf("  Rig:        %s\n", shardCreateRig)
	fmt.Printf("  Namespace:  %s\n", namespace)
	fmt.Printf("  Model:      %s\n", shardCreateModel)
	fmt.Println()
	fmt.Printf("  %s\n", style.Dim.Render("K8s Job creation: pending Daedalus integration"))
	fmt.Printf("\n  Track with: %s\n", style.Dim.Render(fmt.Sprintf("gt shard status %s", shardID)))

	return nil
}

func runShardList(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	townBeads := filepath.Join(townRoot, ".beads")

	// List shard-type issues
	listArgs := []string{"list", "--type=shard", "--json"}
	if !shardListAll {
		listArgs = append(listArgs, "--status=open")
	}

	listCmd := exec.Command("bd", listArgs...)
	listCmd.Dir = townBeads
	var stdout bytes.Buffer
	listCmd.Stdout = &stdout

	if err := listCmd.Run(); err != nil {
		return fmt.Errorf("listing shards: %w", err)
	}

	var shards []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		Description string `json:"description"`
		CreatedAt   string `json:"created_at"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &shards); err != nil {
		return fmt.Errorf("parsing shard list: %w", err)
	}

	// Filter by rig if specified
	if shardListRig != "" {
		var filtered []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Status      string `json:"status"`
			Description string `json:"description"`
			CreatedAt   string `json:"created_at"`
		}
		for _, s := range shards {
			if strings.Contains(s.Description, "Rig: "+shardListRig) {
				filtered = append(filtered, s)
			}
		}
		shards = filtered
	}

	if shardListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(shards)
	}

	if len(shards) == 0 {
		fmt.Println("No shards found.")
		fmt.Println("Create a shard with: gt shard create --objective <obj> --rig <rig>")
		return nil
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Shards"))
	for _, s := range shards {
		status := formatShardStatus(s.Status)
		// Extract rig from description
		rig := extractFromDescription(s.Description, "Rig:")
		rigDisplay := ""
		if rig != "" {
			rigDisplay = fmt.Sprintf(" [%s]", rig)
		}
		fmt.Printf("  🔷 %s: %s%s %s\n", s.ID, s.Title, rigDisplay, status)
	}
	fmt.Printf("\nUse 'gt shard status <id>' for detailed status.\n")

	return nil
}

func runShardStatus(cmd *cobra.Command, args []string) error {
	shardID := args[0]

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	townBeads := filepath.Join(townRoot, ".beads")

	// Get shard details
	showArgs := []string{"show", shardID, "--json"}
	showCmd := exec.Command("bd", showArgs...)
	showCmd.Dir = townBeads
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return fmt.Errorf("shard '%s' not found", shardID)
	}

	var shards []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Status      string `json:"status"`
		Description string `json:"description"`
		CreatedAt   string `json:"created_at"`
		ClosedAt    string `json:"closed_at,omitempty"`
		IssueType   string `json:"issue_type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &shards); err != nil {
		return fmt.Errorf("parsing shard data: %w", err)
	}

	if len(shards) == 0 {
		return fmt.Errorf("shard '%s' not found", shardID)
	}

	shard := shards[0]

	// Verify it's a shard type
	if shard.IssueType != "shard" {
		return fmt.Errorf("'%s' is not a shard (type: %s)", shardID, shard.IssueType)
	}

	// Extract fields from description
	rig := extractFromDescription(shard.Description, "Rig:")
	namespace := extractFromDescription(shard.Description, "Namespace:")
	model := extractFromDescription(shard.Description, "Model:")
	objective := extractFromDescription(shard.Description, "Objective:")

	if shardStatusJSON {
		info := ShardInfo{
			ID:        shard.ID,
			Objective: objective,
			Rig:       rig,
			Namespace: namespace,
			Model:     model,
			Status:    shard.Status,
		}
		if shard.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, shard.CreatedAt); err == nil {
				info.CreatedAt = t
			}
		}
		if shard.ClosedAt != "" {
			if t, err := time.Parse(time.RFC3339, shard.ClosedAt); err == nil {
				info.CompletedAt = t
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	// Human-readable output
	fmt.Printf("🔷 %s %s\n\n", style.Bold.Render(shard.ID+":"), shard.Title)
	fmt.Printf("  Status:     %s\n", formatShardStatus(shard.Status))
	fmt.Printf("  Rig:        %s\n", rig)
	fmt.Printf("  Namespace:  %s\n", namespace)
	fmt.Printf("  Model:      %s\n", model)
	fmt.Printf("  Created:    %s\n", shard.CreatedAt)
	if shard.ClosedAt != "" {
		fmt.Printf("  Completed:  %s\n", shard.ClosedAt)
	}

	fmt.Printf("\n  %s\n", style.Bold.Render("Objective:"))
	fmt.Printf("  %s\n", objective)

	// TODO: Show K8s Job status when Daedalus is integrated
	fmt.Printf("\n  %s\n", style.Dim.Render("K8s Job status: pending Daedalus integration"))

	return nil
}

func runShardAbort(cmd *cobra.Command, args []string) error {
	shardID := args[0]

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	townBeads := filepath.Join(townRoot, ".beads")

	// Verify shard exists and is open
	showArgs := []string{"show", shardID, "--json"}
	showCmd := exec.Command("bd", showArgs...)
	showCmd.Dir = townBeads
	var stdout bytes.Buffer
	showCmd.Stdout = &stdout

	if err := showCmd.Run(); err != nil {
		return fmt.Errorf("shard '%s' not found", shardID)
	}

	var shards []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		IssueType string `json:"issue_type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &shards); err != nil {
		return fmt.Errorf("parsing shard data: %w", err)
	}

	if len(shards) == 0 {
		return fmt.Errorf("shard '%s' not found", shardID)
	}

	shard := shards[0]

	if shard.IssueType != "shard" {
		return fmt.Errorf("'%s' is not a shard (type: %s)", shardID, shard.IssueType)
	}

	if shard.Status == "closed" {
		return fmt.Errorf("shard '%s' is already closed", shardID)
	}

	// Confirmation unless --force
	if !shardAbortForce {
		fmt.Printf("Abort shard %s (%s)? [y/N] ", shardID, shard.Title)
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// TODO: Delete K8s Job via Daedalus API when integrated
	fmt.Printf("  %s\n", style.Dim.Render("K8s Job deletion: pending Daedalus integration"))

	// Close the shard bead
	closeArgs := []string{"close", shardID, "-r", "Aborted by user"}
	closeCmd := exec.Command("bd", closeArgs...)
	closeCmd.Dir = townBeads

	if err := closeCmd.Run(); err != nil {
		return fmt.Errorf("closing shard bead: %w", err)
	}

	fmt.Printf("%s Aborted shard 🔷 %s\n", style.Bold.Render("✓"), shardID)

	return nil
}

// formatShardStatus returns a styled status indicator.
func formatShardStatus(status string) string {
	switch status {
	case "open":
		return style.Warning.Render("● pending")
	case "in_progress":
		return style.Info.Render("▶ running")
	case "closed":
		return style.Success.Render("✓ completed")
	default:
		return status
	}
}

// extractFromDescription extracts a value from a multi-line description.
// Format: "Key: value"
func extractFromDescription(desc, key string) string {
	for _, line := range strings.Split(desc, "\n") {
		if strings.HasPrefix(line, key) {
			return strings.TrimSpace(strings.TrimPrefix(line, key))
		}
	}
	return ""
}


package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Blackboard command flags
var (
	blackboardJSON bool
)

var blackboardCmd = &cobra.Command{
	Use:     "blackboard",
	GroupID: GroupComm,
	Short:   "Shared context for K8s agents and polecats (Cyclopes sync)",
	RunE:    requireSubcommand,
	Long: `Manage the Blackboard - shared context between K8s shards and local polecats.

The Blackboard enables coordination between Cyclopes (K8s agents) and Gas Town
(local polecats). Context written by shards is available to polecats, and
polecat results can be written back to the Blackboard.

WHAT IS THE BLACKBOARD:
  - Shared key-value store attached to work units (shards, convoys)
  - Syncs bidirectionally between K8s and local execution
  - Enables hybrid workflows: shard does heavy lifting, polecat refines

COMMANDS:
  read      Read context from a Blackboard
  write     Write context to a Blackboard
  sync      Sync Blackboard with hook context
  list      List all Blackboards with content`,
}

var blackboardReadCmd = &cobra.Command{
	Use:   "read <shard-or-convoy-id> [key]",
	Short: "Read context from a Blackboard",
	Long: `Read context from a shard or convoy's Blackboard.

Without a key, returns the entire Blackboard as JSON.
With a key, returns just that value.

Examples:
  gt blackboard read hq-sh-abc           # Read entire blackboard
  gt blackboard read hq-sh-abc result    # Read just 'result' key
  gt blackboard read hq-cv-xyz context   # Read convoy context`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runBlackboardRead,
}

var blackboardWriteCmd = &cobra.Command{
	Use:   "write <shard-or-convoy-id> <key> <value>",
	Short: "Write context to a Blackboard",
	Long: `Write a key-value pair to a shard or convoy's Blackboard.

The value can be plain text or JSON. Use --json flag to parse value as JSON.

Examples:
  gt blackboard write hq-sh-abc result "Task completed successfully"
  gt blackboard write hq-sh-abc findings '{"issues": 3, "fixed": 2}'
  gt blackboard write hq-cv-xyz status "Phase 1 complete"`,
	Args: cobra.ExactArgs(3),
	RunE: runBlackboardWrite,
}

var blackboardSyncCmd = &cobra.Command{
	Use:   "sync <shard-id> <direction>",
	Short: "Sync Blackboard with hook context",
	Long: `Sync Blackboard context bidirectionally with Gas Town hooks.

Direction can be:
  to-hook    Copy Blackboard context to the current agent's hook context
  from-hook  Copy hook context back to the Blackboard

This enables:
- Shards to provide context to polecats (to-hook)
- Polecats to report results to shards (from-hook)

Examples:
  gt blackboard sync hq-sh-abc to-hook     # Get shard context into my hook
  gt blackboard sync hq-sh-abc from-hook   # Push my results to shard`,
	Args: cobra.ExactArgs(2),
	RunE: runBlackboardSync,
}

var blackboardListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all Blackboards with content",
	Long: `List all shards and convoys that have Blackboard content.

Shows which work units have shared context available for coordination.

Examples:
  gt blackboard list
  gt blackboard list --json`,
	RunE: runBlackboardList,
}

func init() {
	// Read flags
	blackboardReadCmd.Flags().BoolVar(&blackboardJSON, "json", false, "Output as JSON")

	// List flags
	blackboardListCmd.Flags().BoolVar(&blackboardJSON, "json", false, "Output as JSON")

	// Add subcommands
	blackboardCmd.AddCommand(blackboardReadCmd)
	blackboardCmd.AddCommand(blackboardWriteCmd)
	blackboardCmd.AddCommand(blackboardSyncCmd)
	blackboardCmd.AddCommand(blackboardListCmd)

	rootCmd.AddCommand(blackboardCmd)
}

// BlackboardData represents the content of a Blackboard
type BlackboardData map[string]interface{}

func runBlackboardRead(cmd *cobra.Command, args []string) error {
	targetID := args[0]
	var key string
	if len(args) > 1 {
		key = args[1]
	}

	townBeads, err := getTownBeadsDir()
	if err != nil {
		return err
	}

	// Read blackboard slot from the target
	data, err := readBlackboard(townBeads, targetID)
	if err != nil {
		return err
	}

	if data == nil || len(data) == 0 {
		if blackboardJSON {
			fmt.Println("{}")
		} else {
			fmt.Println("Blackboard is empty.")
		}
		return nil
	}

	// If specific key requested
	if key != "" {
		val, ok := data[key]
		if !ok {
			return fmt.Errorf("key '%s' not found in blackboard", key)
		}
		if blackboardJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(val)
		}
		fmt.Println(val)
		return nil
	}

	// Return entire blackboard
	if blackboardJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Blackboard: "+targetID))
	for k, v := range data {
		fmt.Printf("  %s: %v\n", k, v)
	}
	return nil
}

func runBlackboardWrite(cmd *cobra.Command, args []string) error {
	targetID := args[0]
	key := args[1]
	value := args[2]

	townBeads, err := getTownBeadsDir()
	if err != nil {
		return err
	}

	// Read existing blackboard
	data, err := readBlackboard(townBeads, targetID)
	if err != nil {
		// If not found, start fresh
		data = make(BlackboardData)
	}
	if data == nil {
		data = make(BlackboardData)
	}

	// Try to parse value as JSON, otherwise store as string
	var parsedValue interface{}
	if err := json.Unmarshal([]byte(value), &parsedValue); err == nil {
		data[key] = parsedValue
	} else {
		data[key] = value
	}

	// Write back
	if err := writeBlackboard(townBeads, targetID, data); err != nil {
		return err
	}

	fmt.Printf("%s Wrote '%s' to blackboard %s\n", style.Bold.Render("✓"), key, targetID)
	return nil
}

func runBlackboardSync(cmd *cobra.Command, args []string) error {
	shardID := args[0]
	direction := args[1]

	if direction != "to-hook" && direction != "from-hook" {
		return fmt.Errorf("direction must be 'to-hook' or 'from-hook'")
	}

	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	townBeads := filepath.Join(townRoot, ".beads")

	if direction == "to-hook" {
		// Read shard blackboard and make available to current agent
		data, err := readBlackboard(townBeads, shardID)
		if err != nil {
			return fmt.Errorf("reading shard blackboard: %w", err)
		}

		if data == nil || len(data) == 0 {
			fmt.Println("Shard blackboard is empty, nothing to sync.")
			return nil
		}

		// Write to a local context file that hooks can read
		contextFile := filepath.Join(townRoot, ".blackboard-context.json")
		contextData, _ := json.MarshalIndent(data, "", "  ")
		if err := os.WriteFile(contextFile, contextData, 0644); err != nil {
			return fmt.Errorf("writing context file: %w", err)
		}

		fmt.Printf("%s Synced blackboard from %s to local context\n", style.Bold.Render("✓"), shardID)
		fmt.Printf("  Context available at: %s\n", contextFile)
		fmt.Printf("  Keys: %v\n", getKeys(data))
		return nil

	} else {
		// from-hook: Read local context and write to shard blackboard
		contextFile := filepath.Join(townRoot, ".blackboard-context.json")
		contextData, err := os.ReadFile(contextFile)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No local context to sync (run 'gt blackboard sync <id> to-hook' first)")
				return nil
			}
			return fmt.Errorf("reading context file: %w", err)
		}

		var data BlackboardData
		if err := json.Unmarshal(contextData, &data); err != nil {
			return fmt.Errorf("parsing context file: %w", err)
		}

		// Write to shard blackboard
		if err := writeBlackboard(townBeads, shardID, data); err != nil {
			return fmt.Errorf("writing to shard blackboard: %w", err)
		}

		fmt.Printf("%s Synced local context to blackboard %s\n", style.Bold.Render("✓"), shardID)
		fmt.Printf("  Keys: %v\n", getKeys(data))
		return nil
	}
}

func runBlackboardList(cmd *cobra.Command, args []string) error {
	townBeads, err := getTownBeadsDir()
	if err != nil {
		return err
	}

	// List all shards and convoys
	var items []struct {
		ID   string
		Type string
		Data BlackboardData
	}

	// List shards
	shardArgs := []string{"list", "--type=shard", "--json"}
	shardCmd := exec.Command("bd", shardArgs...)
	shardCmd.Dir = townBeads
	var stdout bytes.Buffer
	shardCmd.Stdout = &stdout

	if err := shardCmd.Run(); err == nil {
		var shards []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(stdout.Bytes(), &shards) == nil {
			for _, s := range shards {
				data, _ := readBlackboard(townBeads, s.ID)
				if len(data) > 0 {
					items = append(items, struct {
						ID   string
						Type string
						Data BlackboardData
					}{s.ID, "shard", data})
				}
			}
		}
	}

	// List convoys
	stdout.Reset()
	convoyArgs := []string{"list", "--type=convoy", "--json"}
	convoyCmd := exec.Command("bd", convoyArgs...)
	convoyCmd.Dir = townBeads
	convoyCmd.Stdout = &stdout

	if err := convoyCmd.Run(); err == nil {
		var convoys []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(stdout.Bytes(), &convoys) == nil {
			for _, c := range convoys {
				data, _ := readBlackboard(townBeads, c.ID)
				if len(data) > 0 {
					items = append(items, struct {
						ID   string
						Type string
						Data BlackboardData
					}{c.ID, "convoy", data})
				}
			}
		}
	}

	if blackboardJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	if len(items) == 0 {
		fmt.Println("No blackboards with content found.")
		return nil
	}

	fmt.Printf("%s\n\n", style.Bold.Render("Blackboards with Content"))
	for _, item := range items {
		icon := "🔷"
		if item.Type == "convoy" {
			icon = "🚚"
		}
		fmt.Printf("  %s %s (%s)\n", icon, item.ID, item.Type)
		for k := range item.Data {
			fmt.Printf("      • %s\n", k)
		}
	}
	return nil
}

// readBlackboard reads the blackboard slot from a bead
func readBlackboard(townBeads, targetID string) (BlackboardData, error) {
	slotCmd := exec.Command("bd", "slot", "get", targetID, "blackboard")
	slotCmd.Dir = townBeads
	var stdout bytes.Buffer
	slotCmd.Stdout = &stdout

	if err := slotCmd.Run(); err != nil {
		// Slot doesn't exist = empty blackboard
		return nil, nil
	}

	slotValue := strings.TrimSpace(stdout.String())
	if slotValue == "" || slotValue == "null" {
		return nil, nil
	}

	var data BlackboardData
	if err := json.Unmarshal([]byte(slotValue), &data); err != nil {
		return nil, fmt.Errorf("parsing blackboard data: %w", err)
	}

	return data, nil
}

// writeBlackboard writes the blackboard slot to a bead
func writeBlackboard(townBeads, targetID string, data BlackboardData) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling blackboard data: %w", err)
	}

	slotCmd := exec.Command("bd", "slot", "set", targetID, "blackboard", string(jsonData))
	slotCmd.Dir = townBeads
	var stderr bytes.Buffer
	slotCmd.Stderr = &stderr

	if err := slotCmd.Run(); err != nil {
		return fmt.Errorf("setting blackboard slot: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// getKeys returns the keys from a map
func getKeys(data BlackboardData) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

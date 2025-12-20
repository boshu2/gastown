package beads

import "testing"

func TestBuiltinMolecules(t *testing.T) {
	molecules := BuiltinMolecules()

	if len(molecules) != 6 {
		t.Errorf("expected 6 built-in molecules, got %d", len(molecules))
	}

	// Verify each molecule can be parsed and validated
	for _, mol := range molecules {
		t.Run(mol.Title, func(t *testing.T) {
			// Check required fields
			if mol.ID == "" {
				t.Error("molecule missing ID")
			}
			if mol.Title == "" {
				t.Error("molecule missing Title")
			}
			if mol.Description == "" {
				t.Error("molecule missing Description")
			}

			// Parse the molecule steps
			steps, err := ParseMoleculeSteps(mol.Description)
			if err != nil {
				t.Fatalf("failed to parse molecule steps: %v", err)
			}

			if len(steps) == 0 {
				t.Error("molecule has no steps")
			}

			// Validate the molecule as if it were an issue
			issue := &Issue{
				Type:        "molecule",
				Title:       mol.Title,
				Description: mol.Description,
			}

			if err := ValidateMolecule(issue); err != nil {
				t.Errorf("molecule validation failed: %v", err)
			}
		})
	}
}

func TestEngineerInBoxMolecule(t *testing.T) {
	mol := EngineerInBoxMolecule()

	steps, err := ParseMoleculeSteps(mol.Description)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should have 5 steps: design, implement, review, test, submit
	if len(steps) != 5 {
		t.Errorf("expected 5 steps, got %d", len(steps))
	}

	// Verify step refs
	expectedRefs := []string{"design", "implement", "review", "test", "submit"}
	for i, expected := range expectedRefs {
		if steps[i].Ref != expected {
			t.Errorf("step %d: expected ref %q, got %q", i, expected, steps[i].Ref)
		}
	}

	// Verify dependencies
	// design has no deps
	if len(steps[0].Needs) != 0 {
		t.Errorf("design should have no deps, got %v", steps[0].Needs)
	}

	// implement needs design
	if len(steps[1].Needs) != 1 || steps[1].Needs[0] != "design" {
		t.Errorf("implement should need design, got %v", steps[1].Needs)
	}

	// review needs implement
	if len(steps[2].Needs) != 1 || steps[2].Needs[0] != "implement" {
		t.Errorf("review should need implement, got %v", steps[2].Needs)
	}

	// test needs implement
	if len(steps[3].Needs) != 1 || steps[3].Needs[0] != "implement" {
		t.Errorf("test should need implement, got %v", steps[3].Needs)
	}

	// submit needs review and test
	if len(steps[4].Needs) != 2 {
		t.Errorf("submit should need 2 deps, got %v", steps[4].Needs)
	}
}

func TestQuickFixMolecule(t *testing.T) {
	mol := QuickFixMolecule()

	steps, err := ParseMoleculeSteps(mol.Description)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should have 3 steps: implement, test, submit
	if len(steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(steps))
	}

	expectedRefs := []string{"implement", "test", "submit"}
	for i, expected := range expectedRefs {
		if steps[i].Ref != expected {
			t.Errorf("step %d: expected ref %q, got %q", i, expected, steps[i].Ref)
		}
	}
}

func TestResearchMolecule(t *testing.T) {
	mol := ResearchMolecule()

	steps, err := ParseMoleculeSteps(mol.Description)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should have 2 steps: investigate, document
	if len(steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(steps))
	}

	expectedRefs := []string{"investigate", "document"}
	for i, expected := range expectedRefs {
		if steps[i].Ref != expected {
			t.Errorf("step %d: expected ref %q, got %q", i, expected, steps[i].Ref)
		}
	}

	// document needs investigate
	if len(steps[1].Needs) != 1 || steps[1].Needs[0] != "investigate" {
		t.Errorf("document should need investigate, got %v", steps[1].Needs)
	}
}

func TestInstallGoBinaryMolecule(t *testing.T) {
	mol := InstallGoBinaryMolecule()

	if mol.ID != "mol-install-go-binary" {
		t.Errorf("expected ID 'mol-install-go-binary', got %q", mol.ID)
	}

	if mol.Title != "Install Go Binary" {
		t.Errorf("expected Title 'Install Go Binary', got %q", mol.Title)
	}

	steps, err := ParseMoleculeSteps(mol.Description)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should have 1 step: install
	if len(steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(steps))
	}

	if steps[0].Ref != "install" {
		t.Errorf("expected ref 'install', got %q", steps[0].Ref)
	}

	// install has no deps
	if len(steps[0].Needs) != 0 {
		t.Errorf("install should have no deps, got %v", steps[0].Needs)
	}
}

func TestPolecatWorkMolecule(t *testing.T) {
	mol := PolecatWorkMolecule()

	if mol.ID != "mol-polecat-work" {
		t.Errorf("expected ID 'mol-polecat-work', got %q", mol.ID)
	}

	if mol.Title != "Polecat Work" {
		t.Errorf("expected Title 'Polecat Work', got %q", mol.Title)
	}

	steps, err := ParseMoleculeSteps(mol.Description)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should have 8 steps: load-context, implement, self-review, verify-tests,
	// rebase-main, submit-merge, update-handoff, request-shutdown
	if len(steps) != 8 {
		t.Errorf("expected 8 steps, got %d", len(steps))
	}

	expectedRefs := []string{
		"load-context", "implement", "self-review", "verify-tests",
		"rebase-main", "submit-merge", "update-handoff", "request-shutdown",
	}
	for i, expected := range expectedRefs {
		if i >= len(steps) {
			t.Errorf("missing step %d: expected %q", i, expected)
			continue
		}
		if steps[i].Ref != expected {
			t.Errorf("step %d: expected ref %q, got %q", i, expected, steps[i].Ref)
		}
	}

	// Verify key dependencies
	// load-context has no deps
	if len(steps[0].Needs) != 0 {
		t.Errorf("load-context should have no deps, got %v", steps[0].Needs)
	}

	// implement needs load-context
	if len(steps[1].Needs) != 1 || steps[1].Needs[0] != "load-context" {
		t.Errorf("implement should need load-context, got %v", steps[1].Needs)
	}

	// rebase-main needs self-review and verify-tests
	if len(steps[4].Needs) != 2 {
		t.Errorf("rebase-main should need 2 deps, got %v", steps[4].Needs)
	}

	// request-shutdown needs update-handoff
	if len(steps[7].Needs) != 1 || steps[7].Needs[0] != "update-handoff" {
		t.Errorf("request-shutdown should need update-handoff, got %v", steps[7].Needs)
	}
}

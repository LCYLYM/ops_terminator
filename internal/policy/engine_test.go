package policy

import (
	"testing"

	"osagentmvp/internal/models"
)

func TestPolicyAllowsReadOnlyTool(t *testing.T) {
	engine := New()
	result := engine.Check(models.ActionPreview{ToolName: "disk_inspect", ReadOnly: true})
	if result.Decision != models.PolicyDecisionAllow {
		t.Fatalf("unexpected decision: %+v", result)
	}
}

func TestPolicyDeniesDestructiveShell(t *testing.T) {
	engine := New()
	result := engine.Check(models.ActionPreview{ToolName: "run_shell", CommandPreview: "rm -rf /tmp/demo && rm -rf /"})
	if result.Decision != models.PolicyDecisionDeny {
		t.Fatalf("unexpected decision: %+v", result)
	}
}

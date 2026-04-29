package context

import (
	"context"
	"strings"
	"time"

	"osagentmvp/internal/builtin"
	"osagentmvp/internal/models"
	"osagentmvp/internal/policy"
	"osagentmvp/internal/skills"
)

type Builder struct {
	skills *skills.Catalog
	tools  *builtin.Registry
	policy *policy.Engine
}

func NewBuilder(skillCatalog *skills.Catalog, tools *builtin.Registry, policy *policy.Engine) *Builder {
	if skillCatalog == nil {
		skillCatalog = skills.Empty()
	}
	return &Builder{skills: skillCatalog, tools: tools, policy: policy}
}

func (b *Builder) Build(host models.Host, session models.Session, userInput string, operator models.OperatorProfile, knowledge []models.KnowledgeItem) models.ContextSnapshot {
	return models.ContextSnapshot{
		HostID:             host.ID,
		HostDisplayName:    host.DisplayName,
		HostMode:           host.Mode,
		SessionSummary:     session.Summary,
		HostProfileSummary: session.Memory.HostProfile.Summary,
		RollingSummary:     session.Memory.RollingSummary,
		OlderUserLedger:    append([]string(nil), session.Memory.OlderUserLedger...),
		OpenThreads:        append([]string(nil), session.Memory.OpenThreads...),
		OperatorProfile:    operator,
		PolicySummary:      b.policy.Summary(),
		SkillSummaries:     b.skills.Select(userInput, 4),
		KnowledgeMatches:   append([]models.KnowledgeItem(nil), knowledge...),
		BuiltinSummaries:   b.tools.Summaries(),
	}
}

func (b *Builder) EnsureHostProfile(ctx context.Context, host models.Host, memory models.MemoryState, settings models.RuntimeSettings) (models.MemoryState, error) {
	now := time.Now().UTC()
	if memory.LastHostProfileAt != nil {
		expiresAt := memory.LastHostProfileAt.Add(time.Duration(settings.HostProfileTTLMinutes) * time.Minute)
		if strings.TrimSpace(memory.HostProfile.Summary) != "" && now.Before(expiresAt) {
			memory.ProfileStale = false
			memory.HostProfile.Stale = false
			return memory, nil
		}
	}

	profile, err := b.tools.ProbeHostProfile(ctx, host)
	if err != nil {
		if strings.TrimSpace(memory.HostProfile.Summary) != "" {
			memory.ProfileStale = true
			memory.HostProfile.Stale = true
			return memory, nil
		}
		return memory, err
	}

	memory.HostProfile = profile
	memory.LastHostProfileAt = &now
	memory.ProfileStale = false
	return memory, nil
}

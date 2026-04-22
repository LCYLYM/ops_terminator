package context

import (
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

func NewBuilder(skills *skills.Catalog, tools *builtin.Registry, policy *policy.Engine) *Builder {
	return &Builder{skills: skills, tools: tools, policy: policy}
}

func (b *Builder) Build(host models.Host, session models.Session, userInput string) models.ContextSnapshot {
	return models.ContextSnapshot{
		HostID:           host.ID,
		HostDisplayName:  host.DisplayName,
		HostMode:         host.Mode,
		SessionSummary:   session.Summary,
		PolicySummary:    b.policy.Summary(),
		SkillSummaries:   b.skills.Select(userInput, 4),
		BuiltinSummaries: b.tools.Summaries(),
	}
}

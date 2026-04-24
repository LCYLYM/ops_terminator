package store

import "osagentmvp/internal/models"

type Store interface {
	ListHosts() ([]models.Host, error)
	GetHost(id string) (models.Host, bool, error)
	SaveHost(host models.Host) error

	GetGatewayConfig() (models.GatewayConfig, bool, error)
	SaveGatewayConfig(config models.GatewayConfig) error

	ListSessions() ([]models.Session, error)
	GetSession(id string) (models.Session, bool, error)
	SaveSession(session models.Session) error

	ListTurns() ([]models.Turn, error)
	GetTurn(id string) (models.Turn, bool, error)
	SaveTurn(turn models.Turn) error

	ListRuns() ([]models.Run, error)
	GetRun(id string) (models.Run, bool, error)
	SaveRun(run models.Run) error

	ListApprovals() ([]models.Approval, error)
	GetApproval(id string) (models.Approval, bool, error)
	SaveApproval(approval models.Approval) error

	ListAutomations() ([]models.AutomationRule, error)
	GetAutomation(id string) (models.AutomationRule, bool, error)
	SaveAutomation(rule models.AutomationRule) error
	DeleteAutomation(id string) error

	AppendEvent(event models.Event) error
	ListEventsByRun(runID string) ([]models.Event, error)
	AppendAudit(entry models.AuditEntry) error
}

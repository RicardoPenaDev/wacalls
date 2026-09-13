package main

import (
	"context"

	"wacalls/internal/glpi"
	"wacalls/internal/tactical"
)

// supportGLPIClient defines the small consumer interface used by the support service.
type supportGLPIClient interface {
	FindComputerByHostname(ctx context.Context, hostname string) (glpi.Computer, error)
	GetComputer(ctx context.Context, id string) (glpi.Computer, error)
	CreateTicket(ctx context.Context, in glpi.TicketInput) (glpi.CreatedTicket, error)
	GetTicket(ctx context.Context, id string) (glpi.Ticket, error)
}

// supportTacticalClient defines the small consumer interface for read-only Tactical RMM.
type supportTacticalClient interface {
	FindAgentByHostname(ctx context.Context, hostname string) (tactical.Agent, error)
	GetAgent(ctx context.Context, agentID string) (tactical.Agent, error)
}

// supportStoreBackend defines the store interface consumed by the support service.
type supportStoreBackend interface {
	CreateTicketRequest(ctx context.Context, in CreateSupportTicketInput) (*SupportRequest, error)
	GetByID(ctx context.Context, tenantID, id string) (*SupportRequest, error)
	GetByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*SupportRequest, error)
	GetByExternalID(ctx context.Context, tenantID, externalID string) (*SupportRequest, error)
	ListByConversation(ctx context.Context, tenantID, sessionID, chatJID string, limit int) ([]*SupportRequest, error)
	EnrichSnapshot(ctx context.Context, in EnrichSnapshotInput) error
	FinishProcessing(ctx context.Context, in FinishProcessingInput) error
	ClaimRetry(ctx context.Context, in ClaimRetryInput) (*SupportRequest, error)
	RecoverOrphanedProcessing(ctx context.Context, cutoff int64, actorUserID *string) (int, error)
	UpdateLocalDevice(ctx context.Context, in UpdateLocalDeviceInput) error
	Reconcile(ctx context.Context, in ReconcileInput) error
	ListEvents(ctx context.Context, tenantID, supportRequestID string) ([]*SupportRequestEvent, error)
}

// deviceBindingStoreBackend defines the device binding store interface consumed by the support service.
type deviceBindingStoreBackend interface {
	GetForTenant(ctx context.Context, tenantID, id string) (DeviceBinding, error)
	FindByHostname(ctx context.Context, tenantID, hostname string) (DeviceBinding, bool, error)
	Upsert(ctx context.Context, b DeviceBinding) (DeviceBinding, error)
	Search(ctx context.Context, tenantID, query string) ([]DeviceBinding, error)
}

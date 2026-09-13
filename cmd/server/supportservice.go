package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"wacalls/internal/glpi"
)

// SupportService orchestrates support request persistence, equipment resolution,
// GLPI ticket interactions outside transactions, and state finalization.
type SupportService struct {
	store    supportStoreBackend
	bindings deviceBindingStoreBackend
	glpi     supportGLPIClient
	tactical supportTacticalClient
	now      func() time.Time
}

// NewSupportService constructs a new SupportService.
func NewSupportService(
	store supportStoreBackend,
	bindings deviceBindingStoreBackend,
	glpiClient supportGLPIClient,
	tacticalClient supportTacticalClient,
) *SupportService {
	return &SupportService{
		store:    store,
		bindings: bindings,
		glpi:     glpiClient,
		tactical: tacticalClient,
		now:      time.Now,
	}
}

// ServiceCreateTicketInput contains the validated parameters from the caller.
type ServiceCreateTicketInput struct {
	TenantID        string
	OwnerID         string
	ActorUserID     string
	SessionID       string
	ChatJID         string
	IdempotencyKey  string
	RequesterName   string
	Title           string
	Description     string
	DeviceBindingID *string
	Hostname        *string
	CategoryID      *string
	LocationID      *string
	Priority        int
}

// ServiceCreateTicketResult wraps the created or replayed support request and its replay status.
type ServiceCreateTicketResult struct {
	Request  *SupportRequest
	IsReplay bool
}

// ServiceRetryInput specifies the target request for an explicit retry claim.
type ServiceRetryInput struct {
	ID          string
	TenantID    string
	ActorUserID string
}

// ServiceReconcileInput specifies the reconciliation parameters.
type ServiceReconcileInput struct {
	ID           string
	TenantID     string
	Outcome      string // "synced", "safe_to_retry", "processing_orphaned"
	GLPITicketID *string
	ActorUserID  string
	Cutoff       int64
}

// ServiceUpdateDeviceInput specifies parameters for updating local equipment selection.
type ServiceUpdateDeviceInput struct {
	ID              string
	TenantID        string
	DeviceBindingID string
	ActorUserID     string
}

// CreateTicket executes the atomic creation, equipment resolution, GLPI ticket creation
// (strictly outside DB transactions), and CAS finalization.
func (s *SupportService) CreateTicket(ctx context.Context, in ServiceCreateTicketInput) (*ServiceCreateTicketResult, error) {
	if strings.TrimSpace(in.TenantID) == "" {
		return nil, ErrMissingTenantID
	}
	if strings.TrimSpace(in.SessionID) == "" || strings.TrimSpace(in.ChatJID) == "" {
		return nil, ErrMissingSessionOrChat
	}
	if strings.TrimSpace(in.RequesterName) == "" || strings.TrimSpace(in.Title) == "" {
		return nil, ErrMissingRequesterOrTitle
	}
	if in.Priority < 0 || in.Priority > 6 {
		return nil, ErrInvalidPriority
	}
	if err := ValidateIdempotencyKey(in.IdempotencyKey); err != nil {
		return nil, err
	}

	// Validate decimal format of optional category and location IDs
	if in.CategoryID != nil && strings.TrimSpace(*in.CategoryID) != "" {
		n, err := strconv.ParseInt(strings.TrimSpace(*in.CategoryID), 10, 64)
		if err != nil || n <= 0 {
			return nil, errors.New("category id must be a positive decimal")
		}
	}
	if in.LocationID != nil && strings.TrimSpace(*in.LocationID) != "" {
		n, err := strconv.ParseInt(strings.TrimSpace(*in.LocationID), 10, 64)
		if err != nil || n <= 0 {
			return nil, errors.New("location id must be a positive decimal")
		}
	}

	// Validate device binding and hostname
	var resolvedBindingID *string
	var resolvedHostInformed string
	var resolvedGLPICompID *string

	if in.DeviceBindingID != nil && strings.TrimSpace(*in.DeviceBindingID) != "" {
		bID := strings.TrimSpace(*in.DeviceBindingID)
		binding, err := s.bindings.GetForTenant(ctx, in.TenantID, bID)
		if err != nil {
			if errors.Is(err, ErrDeviceBindingNotFound) {
				return nil, ErrDeviceBindingNotFound
			}
			return nil, err
		}
		if in.Hostname != nil && strings.TrimSpace(*in.Hostname) != "" {
			if normalizeHostname(*in.Hostname) != binding.HostnameNormalized {
				return nil, ErrDeviceMismatch
			}
		}
		resolvedBindingID = &binding.ID
		resolvedHostInformed = binding.Hostname
		if binding.GLPIComputerID != "" {
			compID := binding.GLPIComputerID
			resolvedGLPICompID = &compID
		}
	} else if in.Hostname != nil && strings.TrimSpace(*in.Hostname) != "" {
		norm := normalizeHostname(*in.Hostname)
		if norm == "" {
			return nil, ErrInvalidHostname
		}
		resolvedHostInformed = strings.TrimSpace(*in.Hostname)
	}

	// Calculate canonical payload v2 and fingerprint
	var deviceBindingField OptionalField[string]
	if in.DeviceBindingID != nil && strings.TrimSpace(*in.DeviceBindingID) != "" {
		deviceBindingField = OptionalField[string]{Present: true, Value: strings.TrimSpace(*in.DeviceBindingID)}
	}
	var hostnameField OptionalField[string]
	if in.Hostname != nil && strings.TrimSpace(*in.Hostname) != "" {
		hostnameField = OptionalField[string]{Present: true, Value: strings.TrimSpace(*in.Hostname)}
	}
	var categoryField OptionalField[string]
	if in.CategoryID != nil && strings.TrimSpace(*in.CategoryID) != "" {
		categoryField = OptionalField[string]{Present: true, Value: strings.TrimSpace(*in.CategoryID)}
	}
	var locationField OptionalField[string]
	if in.LocationID != nil && strings.TrimSpace(*in.LocationID) != "" {
		locationField = OptionalField[string]{Present: true, Value: strings.TrimSpace(*in.LocationID)}
	}
	var priorityField OptionalField[int]
	if in.Priority > 0 {
		priorityField = OptionalField[int]{Present: true, Value: in.Priority}
	}

	fingerprint, err := CalculatePayloadFingerprintV2(CanonicalPayloadV2{
		V:               2,
		TenantID:        in.TenantID,
		SessionID:       in.SessionID,
		ChatJID:         in.ChatJID,
		DeviceBindingID: deviceBindingField,
		Hostname:        hostnameField,
		RequesterName:   strings.TrimSpace(in.RequesterName),
		Title:           strings.TrimSpace(in.Title),
		Description:     in.Description,
		CategoryID:      categoryField,
		LocationID:      locationField,
		Priority:        priorityField,
	})
	if err != nil {
		return nil, err
	}

	// Generate processing token for this attempt
	myToken, err := GenerateProcessingToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate processing token: %w", err)
	}

	// Transaction 1: Atomic create + claim in store
	req, err := s.store.CreateTicketRequest(ctx, CreateSupportTicketInput{
		OwnerID:                in.OwnerID,
		TenantID:               in.TenantID,
		SessionID:              in.SessionID,
		ChatJID:                in.ChatJID,
		DeviceBindingID:        resolvedBindingID,
		HostnameInformed:       resolvedHostInformed,
		TicketDeviceBindingID:  resolvedBindingID,
		TicketHostnameInformed: nullStringPtr(resolvedHostInformed),
		TicketGLPIComputerID:   resolvedGLPICompID,
		RequesterName:          in.RequesterName,
		Title:                  in.Title,
		Description:            in.Description,
		CategoryID:             in.CategoryID,
		LocationID:             in.LocationID,
		Priority:               in.Priority,
		IdempotencyKey:         in.IdempotencyKey,
		PayloadFingerprint:     fingerprint,
		ActorUserID:            in.ActorUserID,
		ProcessingToken:        myToken,
	})
	if err != nil {
		return nil, err
	}

	// If the token returned by the store differs from myToken, this was a concurrent or replay request.
	// We MUST NOT call CreateTicket, and must return the replay result immediately.
	if req.ProcessingToken != myToken {
		return &ServiceCreateTicketResult{Request: req, IsReplay: true}, nil
	}

	// Equipment resolution (outside transaction)
	if resolvedGLPICompID == nil && resolvedHostInformed != "" {
		normHost := normalizeHostname(resolvedHostInformed)
		// Check local store first
		if localBinding, found, err := s.bindings.FindByHostname(ctx, in.TenantID, normHost); err == nil && found && localBinding.GLPIComputerID != "" {
			compID := localBinding.GLPIComputerID
			resolvedGLPICompID = &compID
		} else {
			// Best-effort remote lookups
			var foundTactical bool
			var foundGLPI bool
			var glpiCompID string

			if s.tactical != nil {
				if _, tacErr := s.tactical.FindAgentByHostname(ctx, normHost); tacErr == nil {
					foundTactical = true
				}
			}
			if s.glpi != nil {
				if comp, glpiErr := s.glpi.FindComputerByHostname(ctx, normHost); glpiErr == nil && comp.ID != "" {
					foundGLPI = true
					glpiCompID = comp.ID
					resolvedGLPICompID = &comp.ID
				}
			}

			// Apply equipment matrix and update local device_bindings
			var matchStatus string
			switch {
			case foundTactical && foundGLPI:
				matchStatus = "matched"
			case foundTactical && !foundGLPI:
				matchStatus = "missing_glpi"
			case !foundTactical && foundGLPI:
				matchStatus = "missing_tactical"
			default:
				matchStatus = "unmatched"
			}

			if foundTactical || foundGLPI {
				_, _ = s.bindings.Upsert(ctx, DeviceBinding{
					OwnerID:            in.OwnerID,
					TenantID:           in.TenantID,
					Hostname:           resolvedHostInformed,
					HostnameNormalized: normHost,
					GLPIComputerID:     glpiCompID,
					MatchStatus:        matchStatus,
					LastVerifiedAt:     s.now().UTC().Unix(),
				})
			}
		}

		if resolvedGLPICompID != nil {
			enrichErr := s.store.EnrichSnapshot(ctx, EnrichSnapshotInput{
				ID:                   req.ID,
				TenantID:             req.TenantID,
				ProcessingToken:      req.ProcessingToken,
				TicketGLPIComputerID: resolvedGLPICompID,
				ActorUserID:          in.ActorUserID,
			})
			if enrichErr != nil {
				// If token was lost to orphan recovery, abort before calling CreateTicket
				if errors.Is(enrichErr, ErrTokenLost) {
					return &ServiceCreateTicketResult{Request: req, IsReplay: false}, ErrTokenLost
				}
				return nil, enrichErr
			}
			req.TicketGLPIComputerID = resolvedGLPICompID
		}
	}

	// Create Ticket in GLPI (outside any DB transaction)
	ticketInput := glpi.TicketInput{
		Name:       req.Title,
		Content:    FormatTicketContent(req),
		Priority:   req.Priority,
		Category:   toIDReference(req.CategoryID),
		Location:   toIDReference(req.LocationID),
		ExternalID: req.ExternalID,
	}

	created, glpiErr := s.glpi.CreateTicket(ctx, ticketInput)

	// Classify outcome and CAS finalize in store
	var targetState SupportSyncState
	var lastErrorCode string
	var ticketID *string
	var ticketHref *string

	if glpiErr == nil {
		targetState = StateSynced
		ticketID = &created.ID
		ticketHref = &created.Href
	} else {
		targetState, lastErrorCode = classifyGLPIError(glpiErr)
	}

	finishErr := s.store.FinishProcessing(ctx, FinishProcessingInput{
		ID:              req.ID,
		TenantID:        req.TenantID,
		ProcessingToken: req.ProcessingToken,
		TargetState:     targetState,
		LastErrorCode:   lastErrorCode,
		GLPITicketID:    ticketID,
		GLPITicketHref:  ticketHref,
		ActorType:       ActorTypeUser,
		ActorUserID:     nullStringPtr(in.ActorUserID),
	})
	if finishErr != nil && !errors.Is(finishErr, ErrTokenLost) {
		return nil, finishErr
	}

	req.SyncState = targetState
	req.LastErrorCode = lastErrorCode
	req.GLPITicketID = ticketID
	req.GLPITicketHref = ticketHref
	req.ProcessingToken = ""

	return &ServiceCreateTicketResult{Request: req, IsReplay: false}, glpiErr
}

// Retry claims and re-attempts ticket creation for requests in retryable_error state.
func (s *SupportService) Retry(ctx context.Context, in ServiceRetryInput) (*SupportRequest, error) {
	existing, err := s.store.GetByID(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	if existing.SyncState != StateRetryableError {
		return nil, ErrStateConflict
	}

	req, err := s.store.ClaimRetry(ctx, ClaimRetryInput{
		ID:          in.ID,
		TenantID:    in.TenantID,
		ActorUserID: in.ActorUserID,
	})
	if err != nil {
		return nil, err
	}

	// Call CreateTicket in GLPI outside transaction
	ticketInput := glpi.TicketInput{
		Name:       req.Title,
		Content:    FormatTicketContent(req),
		Priority:   req.Priority,
		Category:   toIDReference(req.CategoryID),
		Location:   toIDReference(req.LocationID),
		ExternalID: req.ExternalID,
	}

	created, glpiErr := s.glpi.CreateTicket(ctx, ticketInput)

	var targetState SupportSyncState
	var lastErrorCode string
	var ticketID *string
	var ticketHref *string

	if glpiErr == nil {
		targetState = StateSynced
		ticketID = &created.ID
		ticketHref = &created.Href
	} else {
		targetState, lastErrorCode = classifyGLPIError(glpiErr)
	}

	finishErr := s.store.FinishProcessing(ctx, FinishProcessingInput{
		ID:              req.ID,
		TenantID:        req.TenantID,
		ProcessingToken: req.ProcessingToken,
		TargetState:     targetState,
		LastErrorCode:   lastErrorCode,
		GLPITicketID:    ticketID,
		GLPITicketHref:  ticketHref,
		ActorType:       ActorTypeUser,
		ActorUserID:     nullStringPtr(in.ActorUserID),
	})
	if finishErr != nil && !errors.Is(finishErr, ErrTokenLost) {
		return nil, finishErr
	}

	req.SyncState = targetState
	req.LastErrorCode = lastErrorCode
	req.GLPITicketID = ticketID
	req.GLPITicketHref = ticketHref
	req.ProcessingToken = ""

	return req, glpiErr
}

// Reconcile executes manual reconciliation by an authorized administrator.
func (s *SupportService) Reconcile(ctx context.Context, in ServiceReconcileInput) (*SupportRequest, error) {
	req, err := s.store.GetByID(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}

	switch in.Outcome {
	case "synced":
		if req.SyncState != StateUnknown {
			return nil, ErrStateConflict
		}
		if in.GLPITicketID == nil || strings.TrimSpace(*in.GLPITicketID) == "" {
			return nil, errors.New("glpi ticket id required for synced outcome")
		}
		ticketIDStr := strings.TrimSpace(*in.GLPITicketID)

		// Verify ticket exists remotely and matches external_id
		ticket, err := s.glpi.GetTicket(ctx, ticketIDStr)
		if err != nil {
			if errors.Is(err, glpi.ErrNotFound) {
				return nil, ErrTicketNotFound
			}
			return nil, err
		}
		if ticket.ExternalID != req.ExternalID {
			return nil, ErrReconcileExternalIDMismatch
		}

		if err := s.store.Reconcile(ctx, ReconcileInput{
			ID:             req.ID,
			TenantID:       req.TenantID,
			Outcome:        "synced",
			GLPITicketID:   &ticket.ID,
			GLPITicketHref: &ticket.Href,
			ActorUserID:    in.ActorUserID,
		}); err != nil {
			return nil, err
		}

		req.SyncState = StateSynced
		req.GLPITicketID = &ticket.ID
		req.GLPITicketHref = &ticket.Href
		return req, nil

	case "safe_to_retry":
		if req.SyncState != StateUnknown {
			return nil, ErrStateConflict
		}
		if err := s.store.Reconcile(ctx, ReconcileInput{
			ID:          req.ID,
			TenantID:    req.TenantID,
			Outcome:     "safe_to_retry",
			ActorUserID: in.ActorUserID,
		}); err != nil {
			return nil, err
		}
		req.SyncState = StateRetryableError
		return req, nil

	case "processing_orphaned":
		if req.SyncState != StateProcessing {
			return nil, ErrStateConflict
		}
		if req.ProcessingStartedAt > in.Cutoff {
			return nil, ErrStateConflict
		}
		recovered, err := s.store.RecoverOrphanedProcessing(ctx, in.Cutoff, nullStringPtr(in.ActorUserID))
		if err != nil {
			return nil, err
		}
		if recovered == 0 {
			return nil, ErrStateConflict
		}
		req.SyncState = StateUnknown
		return req, nil

	default:
		return nil, ErrInvalidReconcileOutcome
	}
}

// UpdateDevice links or updates the local equipment selection without altering the frozen snapshot.
func (s *SupportService) UpdateDevice(ctx context.Context, in ServiceUpdateDeviceInput) (*SupportRequest, bool, error) {
	req, err := s.store.GetByID(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, false, err
	}

	binding, err := s.bindings.GetForTenant(ctx, in.TenantID, in.DeviceBindingID)
	if err != nil {
		return nil, false, err
	}

	bindingID := binding.ID
	if err := s.store.UpdateLocalDevice(ctx, UpdateLocalDeviceInput{
		ID:               in.ID,
		TenantID:         in.TenantID,
		DeviceBindingID:  &bindingID,
		HostnameInformed: binding.Hostname,
		ActorUserID:      in.ActorUserID,
	}); err != nil {
		return nil, false, err
	}

	req.DeviceBindingID = &binding.ID
	req.HostnameInformed = binding.Hostname
	req.HostnameNormalized = binding.HostnameNormalized

	return req, false, nil
}

// GetByID fetches a support request by tenant and ID.
func (s *SupportService) GetByID(ctx context.Context, tenantID, id string) (*SupportRequest, error) {
	return s.store.GetByID(ctx, tenantID, id)
}

// ListByConversation returns all support requests for a given conversation.
func (s *SupportService) ListByConversation(ctx context.Context, tenantID, sessionID, chatJID string, limit int) ([]*SupportRequest, error) {
	return s.store.ListByConversation(ctx, tenantID, sessionID, chatJID, limit)
}

// classifyGLPIError classifies a GLPI failure into the safe closed state machine.
func classifyGLPIError(err error) (SupportSyncState, string) {
	if err == nil {
		return StateSynced, ""
	}

	var glpiErr *glpi.Error
	if errors.As(err, &glpiErr) {
		if glpiErr.Op == "token" {
			if errors.Is(glpiErr.Kind, glpi.ErrAuth) {
				return StateFailed, "integration_auth_failed"
			}
			return StateRetryableError, "integration_unavailable"
		}

		switch glpiErr.StatusCode {
		case 429:
			return StateRetryableError, "rate_limited"
		case 401:
			return StateRetryableError, "integration_auth_failed"
		case 400, 403, 404, 409, 422:
			return StateFailed, "ticket_rejected"
		default:
			return StateUnknown, "ticket_result_unknown"
		}
	}

	return StateUnknown, "ticket_result_unknown"
}

func toIDReference(id *string) *glpi.IDReference {
	if id == nil || strings.TrimSpace(*id) == "" {
		return nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(*id), 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	return &glpi.IDReference{ID: n}
}

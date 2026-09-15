package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"wacalls/internal/glpi"
	"wacalls/internal/tactical"
)

// SupportService orchestrates support request persistence, equipment resolution,
// GLPI ticket interactions outside transactions, and state finalization.
type SupportService struct {
	store    supportStoreBackend
	bindings deviceBindingStoreBackend
	glpi     supportGLPIClient
	tactical supportTacticalClient
	now      func() time.Time
	log      *slog.Logger
}

// NewSupportService constructs a new SupportService.
func NewSupportService(
	store supportStoreBackend,
	bindings deviceBindingStoreBackend,
	glpiClient supportGLPIClient,
	tacticalClient supportTacticalClient,
	log *slog.Logger,
) *SupportService {
	if log == nil {
		log = slog.Default()
	}
	return &SupportService{
		store:    store,
		bindings: bindings,
		glpi:     glpiClient,
		tactical: tacticalClient,
		now:      time.Now,
		log:      log,
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

// ServiceRefreshDeviceInput specifies parameters for an administrative,
// read-only re-resolution of an existing device binding's GLPI/Tactical ids.
type ServiceRefreshDeviceInput struct {
	ID       string
	TenantID string
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
		localBinding, found, err := s.bindings.FindByHostname(ctx, in.TenantID, norm)
		if err != nil {
			return nil, err
		}
		if found {
			resolvedBindingID = &localBinding.ID
			resolvedHostInformed = localBinding.Hostname
			if localBinding.GLPIComputerID != "" {
				compID := localBinding.GLPIComputerID
				resolvedGLPICompID = &compID
			}
		}
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
	if (resolvedGLPICompID == nil || resolvedBindingID == nil) && resolvedHostInformed != "" {
		normHost := normalizeHostname(resolvedHostInformed)
		// Check local store first if binding not resolved
		if resolvedBindingID == nil {
			localBinding, found, err := s.bindings.FindByHostname(ctx, in.TenantID, normHost)
			if err != nil {
				return nil, err
			}
			if found {
				resolvedBindingID = &localBinding.ID
				if localBinding.GLPIComputerID != "" && resolvedGLPICompID == nil {
					compID := localBinding.GLPIComputerID
					resolvedGLPICompID = &compID
				}
			}
		}
		if resolvedGLPICompID == nil {
			// Best-effort remote lookups
			var foundTactical bool
			var foundGLPI bool
			var glpiCompID string
			var tacticalAgentID string

			if s.tactical != nil {
				agent, tacErr := s.tactical.FindAgentByHostname(ctx, normHost)
				switch {
				case tacErr == nil && agent.AgentID != "":
					foundTactical = true
					tacticalAgentID = agent.AgentID
					if !agent.LastSeenValid {
						s.log.Warn("support: tactical agent located with unparseable last_seen, ignoring stale telemetry",
							"tenant", in.TenantID, "hostname", hashHostnameForLog(normHost))
					}
				case tacErr == nil:
					// Matched by hostname but the upstream response carried
					// no agent_id: not a usable Tactical identity. Kept
					// distinct from "not found" and from a request-level
					// failure (T-007 7.4-R2).
					s.log.Warn("support: tactical agent matched but agent_id is empty, discarding match",
						"tenant", in.TenantID, "hostname", hashHostnameForLog(normHost))
				default:
					category, statusCode := tacticalErrorCategory(tacErr)
					level := slog.LevelWarn
					if category == "not_found" {
						level = slog.LevelInfo
					}
					attrs := []any{
						"tenant", in.TenantID, "hostname", hashHostnameForLog(normHost),
						"operation", "create_ticket", "integration", "tactical",
						"category", category, "httpStatus", statusCode,
					}
					attrs = append(attrs, tacticalDecodeDetails(tacErr)...)
					s.log.Log(ctx, level, "support: tactical lookup did not resolve", attrs...)
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
			matchStatus := equipmentMatchStatus(foundTactical, foundGLPI)

			if foundTactical || foundGLPI {
				savedBinding, err := s.bindings.Upsert(ctx, DeviceBinding{
					OwnerID:            in.OwnerID,
					TenantID:           in.TenantID,
					Hostname:           resolvedHostInformed,
					HostnameNormalized: normHost,
					GLPIComputerID:     glpiCompID,
					TacticalAgentID:    tacticalAgentID,
					MatchStatus:        matchStatus,
					LastVerifiedAt:     s.now().UTC().Unix(),
				})
				if err != nil {
					return nil, err
				}
				if savedBinding.ID != "" {
					resolvedBindingID = &savedBinding.ID
				}
			}
		}

		if resolvedGLPICompID != nil || resolvedBindingID != nil {
			enrichErr := s.store.EnrichSnapshot(ctx, EnrichSnapshotInput{
				ID:                    req.ID,
				TenantID:              req.TenantID,
				ProcessingToken:       req.ProcessingToken,
				TicketGLPIComputerID:  resolvedGLPICompID,
				TicketDeviceBindingID: resolvedBindingID,
				ActorUserID:           in.ActorUserID,
			})
			if enrichErr != nil {
				// If token was lost to orphan recovery, abort before calling CreateTicket
				if errors.Is(enrichErr, ErrTokenLost) {
					return &ServiceCreateTicketResult{Request: req, IsReplay: false}, ErrTokenLost
				}
				return nil, enrichErr
			}
			if resolvedGLPICompID != nil {
				req.TicketGLPIComputerID = resolvedGLPICompID
			}
			if resolvedBindingID != nil {
				req.TicketDeviceBindingID = resolvedBindingID
				if req.DeviceBindingID == nil {
					req.DeviceBindingID = resolvedBindingID
				}
			}
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

// equipmentMatchStatus applies the T-004 equipment matrix: it is the single
// source of truth for device_bindings.match_status, shared by CreateTicket's
// best-effort resolution and RefreshDeviceBinding's explicit re-resolution.
func equipmentMatchStatus(foundTactical, foundGLPI bool) string {
	switch {
	case foundTactical && foundGLPI:
		return "matched"
	case foundTactical && !foundGLPI:
		return "missing_glpi"
	case !foundTactical && foundGLPI:
		return "missing_tactical"
	default:
		return "unmatched"
	}
}

// tacticalErrorCategory maps a Tactical client error onto a small, stable
// set of sanitized categories safe to log (T-007 7.4-R2). It never returns
// or logs the upstream error text, URL, headers, or API key — only the
// *tactical.Error's already-safe Kind/StatusCode fields. statusCode is 0
// when the failure never produced an HTTP response (timeout, cancellation,
// network, config).
func tacticalErrorCategory(err error) (category string, statusCode int) {
	if err == nil {
		return "", 0
	}
	var tacErr *tactical.Error
	if !errors.As(err, &tacErr) {
		return "unknown", 0
	}
	statusCode = tacErr.StatusCode
	switch {
	case errors.Is(tacErr.Kind, tactical.ErrNotFound):
		return "not_found", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrAuth):
		return "auth", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrRateLimited):
		return "rate_limited", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrTimeout):
		return "timeout", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrCanceled):
		return "canceled", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrUnavailable):
		return "unavailable", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrBadResponse):
		return "parse_error", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrConflict):
		return "conflict", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrAmbiguous):
		return "ambiguous", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrBadRequest):
		return "bad_request", statusCode
	case errors.Is(tacErr.Kind, tactical.ErrConfig):
		return "config", statusCode
	default:
		return "unknown", statusCode
	}
}

// tacticalDecodeDetails returns extra sanitized log attributes when the
// failure was a JSON decode/shape mismatch (T-007 7.4-R3) — the struct
// field's JSON name, the Go type expected, the JSON kind actually received
// ("string", "number", "bool", "array", "object", "null" — never a value),
// and a byte offset. Returns nil when not applicable. Never includes a
// value, the response body, headers, URL, or the API key.
func tacticalDecodeDetails(err error) []any {
	var tacErr *tactical.Error
	if !errors.As(err, &tacErr) {
		return nil
	}
	if tacErr.DecodeField == "" && tacErr.DecodeGoType == "" && tacErr.DecodeJSONType == "" && tacErr.DecodeOffset == 0 {
		return nil
	}
	var attrs []any
	if tacErr.DecodeField != "" {
		attrs = append(attrs, "decodeField", tacErr.DecodeField)
	}
	if tacErr.DecodeGoType != "" {
		attrs = append(attrs, "decodeGoType", tacErr.DecodeGoType)
	}
	if tacErr.DecodeJSONType != "" {
		attrs = append(attrs, "decodeJSONType", tacErr.DecodeJSONType)
	}
	if tacErr.DecodeOffset != 0 {
		attrs = append(attrs, "decodeOffset", tacErr.DecodeOffset)
	}
	return attrs
}

// hashHostnameForLog returns a short, non-reversible correlation token for
// a hostname. It is safe to log: the original hostname cannot be recovered
// from it, while occurrences of the same hostname remain recognizable
// across log lines (T-007 7.4-R2 — logs must never carry a raw hostname).
func hashHostnameForLog(hostname string) string {
	sum := sha256.Sum256([]byte(normalizeHostname(hostname)))
	return "h:" + hex.EncodeToString(sum[:])[:12]
}

// RefreshDeviceBinding re-runs read-only GLPI/Tactical hostname lookups for
// an existing device binding (T-007 7.4-R1) and merges any newly confirmed
// identifiers. The hostname is always the one already persisted on the
// binding — callers must not accept an arbitrary hostname from a request
// body. It issues no writes to GLPI or Tactical.
//
// Upsert only overwrites glpi_computer_id/tactical_agent_id when the new
// value is non-empty (see devicebindingstore.go), so a lookup that errors or
// finds nothing this round leaves the previously stored identifier exactly
// as it was — it is never blanked out. match_status is recomputed
// deterministically from this call's findings alone.
func (s *SupportService) RefreshDeviceBinding(ctx context.Context, in ServiceRefreshDeviceInput) (DeviceBinding, error) {
	existing, err := s.bindings.GetForTenant(ctx, in.TenantID, in.ID)
	if err != nil {
		return DeviceBinding{}, err
	}

	normHost := normalizeHostname(existing.Hostname)
	if normHost == "" {
		return DeviceBinding{}, ErrInvalidHostname
	}

	var foundTactical, foundGLPI bool
	var tacticalAgentID, glpiCompID string

	if s.tactical != nil {
		agent, tacErr := s.tactical.FindAgentByHostname(ctx, normHost)
		switch {
		case tacErr == nil && agent.AgentID != "":
			foundTactical = true
			tacticalAgentID = agent.AgentID
			if !agent.LastSeenValid {
				s.log.Warn("support: device refresh located tactical agent with unparseable last_seen, ignoring stale telemetry",
					"tenant", in.TenantID, "bindingId", existing.ID, "hostname", hashHostnameForLog(normHost))
			}
		case tacErr == nil:
			// Matched by hostname but the upstream response carried no
			// agent_id: not a usable Tactical identity. Kept distinct from
			// "not found" and from a request-level failure (T-007 7.4-R2).
			s.log.Warn("support: device refresh located tactical agent with empty agent_id, discarding match",
				"tenant", in.TenantID, "bindingId", existing.ID, "hostname", hashHostnameForLog(normHost))
		default:
			category, statusCode := tacticalErrorCategory(tacErr)
			level := slog.LevelWarn
			if category == "not_found" {
				level = slog.LevelInfo
			}
			attrs := []any{
				"tenant", in.TenantID, "bindingId", existing.ID, "hostname", hashHostnameForLog(normHost),
				"operation", "refresh_device_binding", "integration", "tactical",
				"category", category, "httpStatus", statusCode,
			}
			attrs = append(attrs, tacticalDecodeDetails(tacErr)...)
			s.log.Log(ctx, level, "support: device refresh tactical lookup did not resolve", attrs...)
		}
	}
	if s.glpi != nil {
		if comp, glpiErr := s.glpi.FindComputerByHostname(ctx, normHost); glpiErr == nil && comp.ID != "" {
			foundGLPI = true
			glpiCompID = comp.ID
		}
	}

	updated, err := s.bindings.Upsert(ctx, DeviceBinding{
		ID:                 existing.ID,
		OwnerID:            existing.OwnerID,
		TenantID:           existing.TenantID,
		Hostname:           existing.Hostname,
		HostnameNormalized: normHost,
		GLPIComputerID:     glpiCompID,
		TacticalAgentID:    tacticalAgentID,
		TacticalClientID:   existing.TacticalClientID,
		TacticalSiteID:     existing.TacticalSiteID,
		SectorCode:         existing.SectorCode,
		Patrimonio:         existing.Patrimonio,
		MatchStatus:        equipmentMatchStatus(foundTactical, foundGLPI),
		LastVerifiedAt:     s.now().UTC().Unix(),
	})
	if err != nil {
		return DeviceBinding{}, err
	}
	return updated, nil
}

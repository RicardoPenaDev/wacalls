package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// SQLDialect specifies the database dialect for table creation and queries.
type SQLDialect string

const (
	DialectSQLite  SQLDialect = "sqlite"
	DialectMariaDB SQLDialect = "mariadb"
)

// SupportSyncState is the closed set of synchronization states for a SupportRequest.
// StateNew is intentionally omitted: new requests are committed directly to StateProcessing
// in an atomic transaction to eliminate orphaned requests.
type SupportSyncState string

const (
	StateProcessing     SupportSyncState = "processing"
	StateSynced         SupportSyncState = "synced"
	StateRetryableError SupportSyncState = "retryable_error"
	StateUnknown        SupportSyncState = "unknown"
	StateFailed         SupportSyncState = "failed"
)

var validSyncStates = map[SupportSyncState]bool{
	StateProcessing:     true,
	StateSynced:         true,
	StateRetryableError: true,
	StateUnknown:        true,
	StateFailed:         true,
}

// SupportActorType identifies whether an action was performed by a human user or an automated system.
type SupportActorType string

const (
	ActorTypeUser   SupportActorType = "user"
	ActorTypeSystem SupportActorType = "system"
)

// SupportAction represents the append-only audit event action.
type SupportAction string

const (
	ActionCreated                    SupportAction = "created"
	ActionTicketClaimed              SupportAction = "ticket_claimed"
	ActionTicketSynced               SupportAction = "ticket_synced"
	ActionTicketFailed               SupportAction = "ticket_failed"
	ActionTicketRetryable            SupportAction = "ticket_retryable"
	ActionTicketUnknown              SupportAction = "ticket_unknown"
	ActionDeviceLinked               SupportAction = "device_linked"
	ActionDeviceReplaced             SupportAction = "device_replaced"
	ActionSnapshotEnriched           SupportAction = "snapshot_enriched"
	ActionProcessingRecoveredUnknown SupportAction = "processing_recovered_unknown"
	ActionReconciledSynced           SupportAction = "reconciled_synced"
	ActionReconciledRetryable        SupportAction = "reconciled_retryable"
)

var validActions = map[SupportAction]bool{
	ActionCreated:                    true,
	ActionTicketClaimed:              true,
	ActionTicketSynced:               true,
	ActionTicketFailed:               true,
	ActionTicketRetryable:            true,
	ActionTicketUnknown:              true,
	ActionDeviceLinked:               true,
	ActionDeviceReplaced:             true,
	ActionSnapshotEnriched:           true,
	ActionProcessingRecoveredUnknown: true,
	ActionReconciledSynced:           true,
	ActionReconciledRetryable:        true,
}

// Domain errors for Support domain persistence.
var (
	ErrSupportRequestNotFound  = errors.New("support request not found")
	ErrIdempotencyKeyReused    = errors.New("idempotency key reused with different payload")
	ErrStateConflict           = errors.New("support request state conflict")
	ErrInvalidState            = errors.New("invalid support request state")
	ErrInvalidIdempotencyKey   = errors.New("invalid idempotency key")
	ErrInvalidFingerprint      = errors.New("invalid payload fingerprint")
	ErrInvalidExternalID       = errors.New("invalid external id")
	ErrInvalidProcessingToken  = errors.New("invalid processing token")
	ErrMissingTenantID         = errors.New("tenant id required")
	ErrMissingSessionOrChat    = errors.New("session id and chat jid required")
	ErrMissingRequesterOrTitle = errors.New("requester name and title required")
	ErrInvalidPriority         = errors.New("priority must be between 0 and 6")
	ErrTokenLost               = errors.New("processing token lost or expired")
	ErrUnsupportedDialect      = errors.New("unsupported sql dialect")
)

// SupportRequest represents the persistent entity of a support ticket request.
type SupportRequest struct {
	ID                       string           `json:"id"`
	OwnerID                  string           `json:"ownerId"`
	TenantID                 string           `json:"tenantId"`
	SessionID                string           `json:"sessionId"`
	ChatJID                  string           `json:"chatJid"`
	DeviceBindingID          *string          `json:"deviceBindingId,omitempty"`
	HostnameInformed         string           `json:"hostnameInformed"`
	HostnameNormalized       string           `json:"hostnameNormalized"`
	TicketDeviceBindingID    *string          `json:"ticketDeviceBindingId,omitempty"`
	TicketHostnameInformed   *string          `json:"ticketHostnameInformed,omitempty"`
	TicketHostnameNormalized *string          `json:"ticketHostnameNormalized,omitempty"`
	TicketGLPIComputerID     *string          `json:"ticketGlpiComputerId,omitempty"`
	RequesterName            string           `json:"requesterName"`
	Title                    string           `json:"title"`
	Description              string           `json:"description"`
	CategoryID               *string          `json:"categoryId,omitempty"`
	LocationID               *string          `json:"locationId,omitempty"`
	Priority                 int              `json:"priority"`
	GLPITicketID             *string          `json:"glpiTicketId,omitempty"`
	GLPITicketHref           *string          `json:"glpiTicketHref,omitempty"`
	ExternalID               string           `json:"externalId"`
	IdempotencyKey           string           `json:"idempotencyKey"`
	PayloadFingerprint       string           `json:"payloadFingerprint"`
	SyncState                SupportSyncState `json:"syncState"`
	LastErrorCode            string           `json:"lastErrorCode,omitempty"`
	AttemptCount             int              `json:"attemptCount"`
	ProcessingToken          string           `json:"-"` // MUST NEVER be leaked in JSON or public API
	ProcessingStartedAt      int64            `json:"processingStartedAt,omitempty"`
	ProcessedAt              int64            `json:"processedAt,omitempty"`
	CreatedAt                int64            `json:"createdAt"`
	UpdatedAt                int64            `json:"updatedAt"`
	SyncedAt                 int64            `json:"syncedAt,omitempty"`
}

// SupportRequestEvent represents an append-only audit event.
type SupportRequestEvent struct {
	ID               string           `json:"id"`
	SupportRequestID string           `json:"supportRequestId"`
	TenantID         string           `json:"tenantId"`
	ActorType        SupportActorType `json:"actorType"`
	ActorUserID      *string          `json:"actorUserId,omitempty"`
	Action           SupportAction    `json:"action"`
	PreviousDeviceID *string          `json:"previousDeviceId,omitempty"`
	DeviceBindingID  *string          `json:"deviceBindingId,omitempty"`
	ResultCode       string           `json:"resultCode"`
	CreatedAt        int64            `json:"createdAt"`
}

// OptionalField models presence-aware fields for canonical payload v2 fingerprinting.
type OptionalField[T any] struct {
	Present bool `json:"present"`
	Value   T    `json:"value"`
}

// CanonicalPayloadV2 is the exact canonical representation used for calculating payload_fingerprint.
type CanonicalPayloadV2 struct {
	V               int                   `json:"v"`
	TenantID        string                `json:"tenantId"`
	SessionID       string                `json:"sessionId"`
	ChatJID         string                `json:"chatJid"`
	DeviceBindingID OptionalField[string] `json:"deviceBindingId"`
	Hostname        OptionalField[string] `json:"hostname"`
	RequesterName   string                `json:"requesterName"`
	Title           string                `json:"title"`
	Description     string                `json:"description"`
	CategoryID      OptionalField[string] `json:"categoryId"`
	LocationID      OptionalField[string] `json:"locationId"`
	Priority        OptionalField[int]    `json:"priority"`
}

// CreateSupportTicketInput carries the validated parameters for creating a new support request.
type CreateSupportTicketInput struct {
	OwnerID                string
	TenantID               string
	SessionID              string
	ChatJID                string
	DeviceBindingID        *string
	HostnameInformed       string
	TicketDeviceBindingID  *string
	TicketHostnameInformed *string
	TicketGLPIComputerID   *string
	RequesterName          string
	Title                  string
	Description            string
	CategoryID             *string
	LocationID             *string
	Priority               int
	IdempotencyKey         string
	PayloadFingerprint     string
	ExternalID             string
	ActorUserID            string
}

// EnrichSnapshotInput specifies snapshot enrichment conditioned on the active processing_token.
type EnrichSnapshotInput struct {
	ID                    string
	TenantID              string
	ProcessingToken       string
	TicketGLPIComputerID  *string
	TicketDeviceBindingID *string
	ActorUserID           string
}

// FinishProcessingInput contains parameters for transitioning out of processing state.
type FinishProcessingInput struct {
	ID              string
	TenantID        string
	ProcessingToken string
	TargetState     SupportSyncState
	LastErrorCode   string
	GLPITicketID    *string
	GLPITicketHref  *string
	ActorType       SupportActorType
	ActorUserID     *string
}

// ClaimRetryInput parameters for claiming a retry on a request in retryable_error.
type ClaimRetryInput struct {
	ID          string
	TenantID    string
	ActorUserID string
}

// UpdateLocalDeviceInput parameters for updating local device selection without modifying attempt snapshot.
type UpdateLocalDeviceInput struct {
	ID               string
	TenantID         string
	DeviceBindingID  *string
	HostnameInformed string
	ActorUserID      string
}

// ReconcileInput parameters for administrative reconciliation.
type ReconcileInput struct {
	ID             string
	TenantID       string
	Outcome        string // "synced" or "safe_to_retry"
	GLPITicketID   *string
	GLPITicketHref *string
	ActorUserID    string
}

// ValidateIdempotencyKey enforces the 16–128 bytes ASCII constraint and safe charset.
func ValidateIdempotencyKey(key string) error {
	if len(key) < 16 || len(key) > 128 {
		return ErrInvalidIdempotencyKey
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '~' || c == ':' || c == '-' {
			continue
		}
		return ErrInvalidIdempotencyKey
	}
	return nil
}

// NormalizeDescription converts CRLF and CR to LF for canonical representation and persistence.
func NormalizeDescription(desc string) string {
	s := strings.ReplaceAll(desc, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// CalculatePayloadFingerprintV2 serializes the canonical struct in exact key order and returns its SHA-256 hex.
func CalculatePayloadFingerprintV2(canonical CanonicalPayloadV2) (string, error) {
	canonical.V = 2
	canonical.Description = NormalizeDescription(canonical.Description)
	if canonical.Hostname.Present {
		canonical.Hostname.Value = normalizeHostname(canonical.Hostname.Value)
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// GenerateExternalID computes the deterministic 40-character external_id per D-015 / T-005 spec:
// external_id = "wacalls-" + first32(lowercase_hex(SHA-256("support-request:v1\n" + tenant_id + "\n" + support_request.id)))
func GenerateExternalID(tenantID, requestID string) string {
	raw := "support-request:v1\n" + strings.TrimSpace(tenantID) + "\n" + strings.TrimSpace(requestID)
	h := sha256.Sum256([]byte(raw))
	hexStr := hex.EncodeToString(h[:])
	return "wacalls-" + hexStr[:32]
}

// GenerateProcessingToken generates a cryptographically secure 32-character lowercase hex token (16 bytes).
func GenerateProcessingToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isValidSyncState(s SupportSyncState) bool {
	return validSyncStates[s]
}

func isValidAction(a SupportAction) bool {
	return validActions[a]
}

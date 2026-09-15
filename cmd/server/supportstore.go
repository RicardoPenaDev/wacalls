package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type supportStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func newSupportStore(ctx context.Context, db *sql.DB, dialect SQLDialect) (*supportStore, error) {
	if dialect == "" {
		dialect = DialectSQLite
	}
	if dialect != DialectSQLite && dialect != DialectMariaDB {
		return nil, ErrUnsupportedDialect
	}

	s := &supportStore{db: db, dialect: dialect}
	if err := s.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize support store schema: %w", err)
	}
	return s, nil
}

// newEventID generates a time-ordered UUIDv7 string for deterministic audit event ordering.
func newEventID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.New().String()
}

func (s *supportStore) initSchema(ctx context.Context) error {
	switch s.dialect {
	case DialectMariaDB:
		// MariaDB 11.4 InnoDB DDL
		ddlReq := `CREATE TABLE IF NOT EXISTS support_requests (
			id VARCHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
			owner_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
			tenant_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
			session_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
			chat_jid VARCHAR(255) COLLATE utf8mb4_bin NOT NULL,
			device_binding_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
			hostname_informed VARCHAR(64) NOT NULL,
			hostname_normalized VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			ticket_device_binding_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
			ticket_hostname_informed VARCHAR(64) NULL,
			ticket_hostname_normalized VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
			ticket_glpi_computer_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
			requester_name VARCHAR(120) NOT NULL,
			title VARCHAR(200) NOT NULL,
			description TEXT NOT NULL,
			category_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
			location_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
			priority SMALLINT UNSIGNED NOT NULL,
			glpi_ticket_id VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
			glpi_ticket_href VARCHAR(512) NULL,
			external_id VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			idempotency_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			payload_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			sync_state VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			last_error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
			attempt_count INT UNSIGNED NOT NULL DEFAULT 1,
			processing_token VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
			processing_started_at BIGINT NOT NULL DEFAULT 0,
			processed_at BIGINT NOT NULL DEFAULT 0,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			synced_at BIGINT NOT NULL DEFAULT 0,
			CONSTRAINT uq_support_requests_tenant_id UNIQUE (tenant_id, id),
			CONSTRAINT uq_support_requests_tenant_idempotency UNIQUE (tenant_id, idempotency_key),
			CONSTRAINT uq_support_requests_tenant_external UNIQUE (tenant_id, external_id),
			CONSTRAINT chk_support_requests_sync_state CHECK (sync_state IN ('processing', 'synced', 'retryable_error', 'unknown', 'failed')),
			INDEX idx_support_requests_tenant_conversation (tenant_id, session_id, chat_jid, created_at),
			INDEX idx_support_requests_tenant_glpi_ticket (tenant_id, glpi_ticket_id),
			INDEX idx_support_requests_tenant_state (tenant_id, sync_state, updated_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;`

		if _, err := s.db.ExecContext(ctx, ddlReq); err != nil {
			return err
		}

		ddlEvents := `CREATE TABLE IF NOT EXISTS support_request_events (
			id VARCHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
			support_request_id VARCHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			tenant_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
			actor_type VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			actor_user_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
			action VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
			previous_device_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
			device_binding_id VARCHAR(128) COLLATE utf8mb4_bin NULL,
			result_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
			created_at BIGINT NOT NULL,
			CONSTRAINT chk_support_request_events_actor_type CHECK (actor_type IN ('user', 'system')),
			CONSTRAINT fk_support_request_events_request FOREIGN KEY (tenant_id, support_request_id)
				REFERENCES support_requests (tenant_id, id)
				ON UPDATE RESTRICT
				ON DELETE RESTRICT,
			INDEX idx_support_request_events_request (tenant_id, support_request_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;`

		if _, err := s.db.ExecContext(ctx, ddlEvents); err != nil {
			return err
		}

	default: // SQLite
		if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON;`); err != nil {
			return err
		}

		ddlReq := `CREATE TABLE IF NOT EXISTS support_requests (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			chat_jid TEXT NOT NULL,
			device_binding_id TEXT,
			hostname_informed TEXT NOT NULL,
			hostname_normalized TEXT NOT NULL,
			ticket_device_binding_id TEXT,
			ticket_hostname_informed TEXT,
			ticket_hostname_normalized TEXT,
			ticket_glpi_computer_id TEXT,
			requester_name TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			category_id TEXT,
			location_id TEXT,
			priority INTEGER NOT NULL,
			glpi_ticket_id TEXT,
			glpi_ticket_href TEXT,
			external_id TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			payload_fingerprint TEXT NOT NULL,
			sync_state TEXT NOT NULL CHECK (sync_state IN ('processing','synced','retryable_error','unknown','failed')),
			last_error_code TEXT NOT NULL DEFAULT '',
			attempt_count INTEGER NOT NULL DEFAULT 1,
			processing_token TEXT NOT NULL DEFAULT '',
			processing_started_at INTEGER NOT NULL DEFAULT 0,
			processed_at INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			synced_at INTEGER NOT NULL DEFAULT 0,
			CONSTRAINT uq_support_requests_tenant_id UNIQUE (tenant_id, id)
		);`
		if _, err := s.db.ExecContext(ctx, ddlReq); err != nil {
			return err
		}

		indexes := []string{
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_support_requests_tenant_id ON support_requests (tenant_id, id);`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_support_requests_tenant_idempotency ON support_requests (tenant_id, idempotency_key);`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_support_requests_tenant_external ON support_requests (tenant_id, external_id);`,
			`CREATE INDEX IF NOT EXISTS idx_support_requests_tenant_conversation ON support_requests (tenant_id, session_id, chat_jid, created_at);`,
			`CREATE INDEX IF NOT EXISTS idx_support_requests_tenant_glpi_ticket ON support_requests (tenant_id, glpi_ticket_id);`,
			`CREATE INDEX IF NOT EXISTS idx_support_requests_tenant_state ON support_requests (tenant_id, sync_state, updated_at);`,
		}
		for _, idx := range indexes {
			if _, err := s.db.ExecContext(ctx, idx); err != nil {
				return err
			}
		}

		ddlEvents := `CREATE TABLE IF NOT EXISTS support_request_events (
			id TEXT PRIMARY KEY,
			support_request_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'system')),
			actor_user_id TEXT,
			action TEXT NOT NULL,
			previous_device_id TEXT,
			device_binding_id TEXT,
			result_code TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			CONSTRAINT fk_support_request_events_request FOREIGN KEY (tenant_id, support_request_id)
				REFERENCES support_requests (tenant_id, id)
				ON UPDATE RESTRICT
				ON DELETE RESTRICT
		);`
		if _, err := s.db.ExecContext(ctx, ddlEvents); err != nil {
			return err
		}

		if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_support_request_events_request ON support_request_events (tenant_id, support_request_id, created_at);`); err != nil {
			return err
		}
	}
	return nil
}

func (s *supportStore) CreateTicketRequest(ctx context.Context, in CreateSupportTicketInput) (*SupportRequest, error) {
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
	if len(in.PayloadFingerprint) != 64 {
		return nil, ErrInvalidFingerprint
	}

	reqID := uuid.New().String()
	token := in.ProcessingToken
	if token == "" {
		var err error
		token, err = GenerateProcessingToken()
		if err != nil {
			return nil, fmt.Errorf("failed to generate processing token: %w", err)
		}
	}

	extID := in.ExternalID
	if strings.TrimSpace(extID) == "" {
		extID = GenerateExternalID(in.TenantID, reqID)
	}

	now := time.Now().UTC().Unix()
	hostnameNorm := normalizeHostname(in.HostnameInformed)

	ticketHostNorm := ""
	if in.TicketHostnameInformed != nil && *in.TicketHostnameInformed != "" {
		ticketHostNorm = normalizeHostname(*in.TicketHostnameInformed)
	}

	req := &SupportRequest{
		ID:                       reqID,
		OwnerID:                  in.OwnerID,
		TenantID:                 in.TenantID,
		SessionID:                in.SessionID,
		ChatJID:                  in.ChatJID,
		DeviceBindingID:          in.DeviceBindingID,
		HostnameInformed:         strings.TrimSpace(in.HostnameInformed),
		HostnameNormalized:       hostnameNorm,
		TicketDeviceBindingID:    in.TicketDeviceBindingID,
		TicketHostnameInformed:   in.TicketHostnameInformed,
		TicketHostnameNormalized: nullStringPtr(ticketHostNorm),
		TicketGLPIComputerID:     in.TicketGLPIComputerID,
		RequesterName:            strings.TrimSpace(in.RequesterName),
		Title:                    strings.TrimSpace(in.Title),
		Description:              NormalizeDescription(in.Description),
		CategoryID:               in.CategoryID,
		LocationID:               in.LocationID,
		Priority:                 in.Priority,
		ExternalID:               extID,
		IdempotencyKey:           in.IdempotencyKey,
		PayloadFingerprint:       in.PayloadFingerprint,
		SyncState:                StateProcessing,
		LastErrorCode:            "",
		AttemptCount:             1,
		ProcessingToken:          token,
		ProcessingStartedAt:      now,
		ProcessedAt:              0,
		CreatedAt:                now,
		UpdatedAt:                now,
		SyncedAt:                 0,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	const insertQuery = `INSERT INTO support_requests (
		id, owner_id, tenant_id, session_id, chat_jid,
		device_binding_id, hostname_informed, hostname_normalized,
		ticket_device_binding_id, ticket_hostname_informed, ticket_hostname_normalized, ticket_glpi_computer_id,
		requester_name, title, description, category_id, location_id, priority,
		glpi_ticket_id, glpi_ticket_href,
		external_id, idempotency_key, payload_fingerprint,
		sync_state, last_error_code, attempt_count,
		processing_token, processing_started_at, processed_at,
		created_at, updated_at, synced_at
	) VALUES (
		?, ?, ?, ?, ?,
		?, ?, ?,
		?, ?, ?, ?,
		?, ?, ?, ?, ?, ?,
		NULL, NULL,
		?, ?, ?,
		?, ?, ?,
		?, ?, ?,
		?, ?, ?
	)`

	_, err = tx.ExecContext(ctx, insertQuery,
		req.ID, req.OwnerID, req.TenantID, req.SessionID, req.ChatJID,
		nullString(req.DeviceBindingID), req.HostnameInformed, req.HostnameNormalized,
		nullString(req.TicketDeviceBindingID), nullString(req.TicketHostnameInformed), nullString(req.TicketHostnameNormalized), nullString(req.TicketGLPIComputerID),
		req.RequesterName, req.Title, req.Description, nullString(req.CategoryID), nullString(req.LocationID), req.Priority,
		req.ExternalID, req.IdempotencyKey, req.PayloadFingerprint,
		string(req.SyncState), req.LastErrorCode, req.AttemptCount,
		req.ProcessingToken, req.ProcessingStartedAt, req.ProcessedAt,
		req.CreatedAt, req.UpdatedAt, req.SyncedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback()
			existing, getErr := s.GetByIdempotencyKey(ctx, in.TenantID, in.IdempotencyKey)
			if getErr == nil && existing != nil {
				if existing.PayloadFingerprint != in.PayloadFingerprint {
					return nil, ErrIdempotencyKeyReused
				}
				return existing, nil
			}
		}
		return nil, err
	}

	// Insert atomic event 1: created
	createdEventID := newEventID()
	const insertEventQuery = `INSERT INTO support_request_events (
		id, support_request_id, tenant_id, actor_type, actor_user_id,
		action, previous_device_id, device_binding_id, result_code, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	actorUserID := nullStringPtr(in.ActorUserID)
	if _, err := tx.ExecContext(ctx, insertEventQuery,
		createdEventID, req.ID, req.TenantID, string(ActorTypeUser), nullString(actorUserID),
		string(ActionCreated), nil, nullString(req.DeviceBindingID), "", now,
	); err != nil {
		return nil, err
	}

	// Insert atomic event 2: ticket_claimed
	claimedEventID := newEventID()
	if _, err := tx.ExecContext(ctx, insertEventQuery,
		claimedEventID, req.ID, req.TenantID, string(ActorTypeUser), nullString(actorUserID),
		string(ActionTicketClaimed), nil, nullString(req.DeviceBindingID), "", now,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return req, nil
}

func (s *supportStore) GetByID(ctx context.Context, tenantID, id string) (*SupportRequest, error) {
	const query = `SELECT id, owner_id, tenant_id, session_id, chat_jid,
		device_binding_id, hostname_informed, hostname_normalized,
		ticket_device_binding_id, ticket_hostname_informed, ticket_hostname_normalized, ticket_glpi_computer_id,
		requester_name, title, description, category_id, location_id, priority,
		glpi_ticket_id, glpi_ticket_href,
		external_id, idempotency_key, payload_fingerprint,
		sync_state, last_error_code, attempt_count,
		processing_token, processing_started_at, processed_at,
		created_at, updated_at, synced_at
	FROM support_requests
	WHERE tenant_id = ? AND id = ?`

	return s.scanRow(s.db.QueryRowContext(ctx, query, tenantID, id))
}

func (s *supportStore) GetByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*SupportRequest, error) {
	const query = `SELECT id, owner_id, tenant_id, session_id, chat_jid,
		device_binding_id, hostname_informed, hostname_normalized,
		ticket_device_binding_id, ticket_hostname_informed, ticket_hostname_normalized, ticket_glpi_computer_id,
		requester_name, title, description, category_id, location_id, priority,
		glpi_ticket_id, glpi_ticket_href,
		external_id, idempotency_key, payload_fingerprint,
		sync_state, last_error_code, attempt_count,
		processing_token, processing_started_at, processed_at,
		created_at, updated_at, synced_at
	FROM support_requests
	WHERE tenant_id = ? AND idempotency_key = ?`

	return s.scanRow(s.db.QueryRowContext(ctx, query, tenantID, idempotencyKey))
}

func (s *supportStore) GetByExternalID(ctx context.Context, tenantID, externalID string) (*SupportRequest, error) {
	const query = `SELECT id, owner_id, tenant_id, session_id, chat_jid,
		device_binding_id, hostname_informed, hostname_normalized,
		ticket_device_binding_id, ticket_hostname_informed, ticket_hostname_normalized, ticket_glpi_computer_id,
		requester_name, title, description, category_id, location_id, priority,
		glpi_ticket_id, glpi_ticket_href,
		external_id, idempotency_key, payload_fingerprint,
		sync_state, last_error_code, attempt_count,
		processing_token, processing_started_at, processed_at,
		created_at, updated_at, synced_at
	FROM support_requests
	WHERE tenant_id = ? AND external_id = ?`

	return s.scanRow(s.db.QueryRowContext(ctx, query, tenantID, externalID))
}

func (s *supportStore) ListByConversation(ctx context.Context, tenantID, sessionID, chatJID string, limit int) ([]*SupportRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	const query = `SELECT id, owner_id, tenant_id, session_id, chat_jid,
		device_binding_id, hostname_informed, hostname_normalized,
		ticket_device_binding_id, ticket_hostname_informed, ticket_hostname_normalized, ticket_glpi_computer_id,
		requester_name, title, description, category_id, location_id, priority,
		glpi_ticket_id, glpi_ticket_href,
		external_id, idempotency_key, payload_fingerprint,
		sync_state, last_error_code, attempt_count,
		processing_token, processing_started_at, processed_at,
		created_at, updated_at, synced_at
	FROM support_requests
	WHERE tenant_id = ? AND session_id = ? AND chat_jid = ?
	ORDER BY created_at DESC, id DESC
	LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, tenantID, sessionID, chatJID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*SupportRequest
	for rows.Next() {
		req, err := s.scanCurrent(rows.Scan)
		if err != nil {
			return nil, err
		}
		results = append(results, req)
	}
	return results, rows.Err()
}

func (s *supportStore) EnrichSnapshot(ctx context.Context, in EnrichSnapshotInput) error {
	if in.ID == "" || in.TenantID == "" || in.ProcessingToken == "" {
		return ErrInvalidProcessingToken
	}

	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const query = `UPDATE support_requests
		SET ticket_glpi_computer_id = ?, ticket_device_binding_id = ?, device_binding_id = COALESCE(device_binding_id, ?), updated_at = ?
		WHERE id = ? AND tenant_id = ? AND sync_state = 'processing' AND processing_token = ?`

	res, err := tx.ExecContext(ctx, query,
		nullString(in.TicketGLPIComputerID), nullString(in.TicketDeviceBindingID), nullString(in.TicketDeviceBindingID), now,
		in.ID, in.TenantID, in.ProcessingToken,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTokenLost
	}

	eventID := newEventID()
	const eventQuery = `INSERT INTO support_request_events (
		id, support_request_id, tenant_id, actor_type, actor_user_id,
		action, previous_device_id, device_binding_id, result_code, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	actorUserID := nullStringPtr(in.ActorUserID)
	if _, err := tx.ExecContext(ctx, eventQuery,
		eventID, in.ID, in.TenantID, string(ActorTypeUser), nullString(actorUserID),
		string(ActionSnapshotEnriched), nil, nullString(in.TicketDeviceBindingID), "", now,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *supportStore) FinishProcessing(ctx context.Context, in FinishProcessingInput) error {
	if in.ID == "" || in.TenantID == "" || in.ProcessingToken == "" {
		return ErrInvalidProcessingToken
	}
	if !isValidSyncState(in.TargetState) || in.TargetState == StateProcessing {
		return ErrInvalidState
	}

	now := time.Now().UTC().Unix()
	var syncedAt int64
	if in.TargetState == StateSynced {
		syncedAt = now
	}

	var action SupportAction
	switch in.TargetState {
	case StateSynced:
		action = ActionTicketSynced
	case StateFailed:
		action = ActionTicketFailed
	case StateRetryableError:
		action = ActionTicketRetryable
	case StateUnknown:
		action = ActionTicketUnknown
	default:
		return ErrInvalidState
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const query = `UPDATE support_requests
		SET sync_state = ?, last_error_code = ?, glpi_ticket_id = ?, glpi_ticket_href = ?,
			synced_at = ?, processing_token = '', processing_started_at = 0,
			processed_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND sync_state = 'processing' AND processing_token = ?`

	res, err := tx.ExecContext(ctx, query,
		string(in.TargetState), in.LastErrorCode, nullString(in.GLPITicketID), nullString(in.GLPITicketHref),
		syncedAt, now, now,
		in.ID, in.TenantID, in.ProcessingToken,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTokenLost
	}

	eventID := newEventID()
	const eventQuery = `INSERT INTO support_request_events (
		id, support_request_id, tenant_id, actor_type, actor_user_id,
		action, previous_device_id, device_binding_id, result_code, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	actorType := in.ActorType
	if actorType == "" {
		actorType = ActorTypeUser
	}

	if _, err := tx.ExecContext(ctx, eventQuery,
		eventID, in.ID, in.TenantID, string(actorType), nullString(in.ActorUserID),
		string(action), nil, nil, in.LastErrorCode, now,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *supportStore) ClaimRetry(ctx context.Context, in ClaimRetryInput) (*SupportRequest, error) {
	if in.ID == "" || in.TenantID == "" {
		return nil, ErrSupportRequestNotFound
	}

	newToken, err := GenerateProcessingToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate processing token: %w", err)
	}

	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	const query = `UPDATE support_requests
		SET sync_state = 'processing', processing_token = ?,
			processing_started_at = ?, processed_at = 0,
			attempt_count = attempt_count + 1, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND sync_state = 'retryable_error' AND processing_token = ''`

	res, err := tx.ExecContext(ctx, query, newToken, now, now, in.ID, in.TenantID)
	if err != nil {
		return nil, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, ErrStateConflict
	}

	eventID := newEventID()
	const eventQuery = `INSERT INTO support_request_events (
		id, support_request_id, tenant_id, actor_type, actor_user_id,
		action, previous_device_id, device_binding_id, result_code, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	actorUserID := nullStringPtr(in.ActorUserID)
	if _, err := tx.ExecContext(ctx, eventQuery,
		eventID, in.ID, in.TenantID, string(ActorTypeUser), nullString(actorUserID),
		string(ActionTicketClaimed), nil, nil, "", now,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	updated, err := s.GetByID(ctx, in.TenantID, in.ID)
	if err != nil {
		return nil, err
	}
	updated.ProcessingToken = newToken
	return updated, nil
}

func (s *supportStore) RecoverOrphanedProcessing(ctx context.Context, cutoff int64, actorUserID *string) (int, error) {
	const findQuery = `SELECT id, tenant_id, processing_token, processing_started_at
		FROM support_requests
		WHERE sync_state = 'processing' AND processing_started_at <= ?`

	rows, err := s.db.QueryContext(ctx, findQuery, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type candidate struct {
		id      string
		tenant  string
		token   string
		started int64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.tenant, &c.token, &c.started); err != nil {
			return 0, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	recovered := 0
	for _, c := range candidates {
		now := time.Now().UTC().Unix()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return recovered, err
		}

		const updateQuery = `UPDATE support_requests
			SET sync_state = 'unknown', processing_token = '',
				processing_started_at = 0, processed_at = ?, updated_at = ?,
				last_error_code = 'orphaned_processing'
			WHERE id = ? AND tenant_id = ? AND sync_state = 'processing'
				AND processing_token = ? AND processing_started_at <= ?`

		res, err := tx.ExecContext(ctx, updateQuery, now, now, c.id, c.tenant, c.token, cutoff)
		if err != nil {
			_ = tx.Rollback()
			continue
		}
		aff, err := res.RowsAffected()
		if err != nil || aff == 0 {
			_ = tx.Rollback()
			continue
		}

		actorType := ActorTypeSystem
		if actorUserID != nil && *actorUserID != "" {
			actorType = ActorTypeUser
		}

		eventID := newEventID()
		const eventQuery = `INSERT INTO support_request_events (
			id, support_request_id, tenant_id, actor_type, actor_user_id,
			action, previous_device_id, device_binding_id, result_code, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		if _, err := tx.ExecContext(ctx, eventQuery,
			eventID, c.id, c.tenant, string(actorType), nullString(actorUserID),
			string(ActionProcessingRecoveredUnknown), nil, nil, "orphaned_processing", now,
		); err != nil {
			_ = tx.Rollback()
			continue
		}

		if err := tx.Commit(); err == nil {
			recovered++
		}
	}

	return recovered, nil
}

func (s *supportStore) UpdateLocalDevice(ctx context.Context, in UpdateLocalDeviceInput) error {
	if in.ID == "" || in.TenantID == "" {
		return ErrSupportRequestNotFound
	}

	current, err := s.GetByID(ctx, in.TenantID, in.ID)
	if err != nil {
		return err
	}

	hostnameNorm := normalizeHostname(in.HostnameInformed)

	// If device and hostname are identical, no-op
	currBinding := ""
	if current.DeviceBindingID != nil {
		currBinding = *current.DeviceBindingID
	}
	newBinding := ""
	if in.DeviceBindingID != nil {
		newBinding = *in.DeviceBindingID
	}
	if currBinding == newBinding && current.HostnameNormalized == hostnameNorm {
		return nil
	}

	now := time.Now().UTC().Unix()
	action := ActionDeviceLinked
	if currBinding != "" {
		action = ActionDeviceReplaced
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const query = `UPDATE support_requests
		SET device_binding_id = ?, hostname_informed = ?, hostname_normalized = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ?`

	res, err := tx.ExecContext(ctx, query,
		nullString(in.DeviceBindingID), strings.TrimSpace(in.HostnameInformed), hostnameNorm, now,
		in.ID, in.TenantID,
	)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrSupportRequestNotFound
	}

	eventID := newEventID()
	const eventQuery = `INSERT INTO support_request_events (
		id, support_request_id, tenant_id, actor_type, actor_user_id,
		action, previous_device_id, device_binding_id, result_code, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	actorUserID := nullStringPtr(in.ActorUserID)
	if _, err := tx.ExecContext(ctx, eventQuery,
		eventID, in.ID, in.TenantID, string(ActorTypeUser), nullString(actorUserID),
		string(action), nullString(current.DeviceBindingID), nullString(in.DeviceBindingID), "", now,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *supportStore) Reconcile(ctx context.Context, in ReconcileInput) error {
	if in.ID == "" || in.TenantID == "" {
		return ErrSupportRequestNotFound
	}

	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var action SupportAction

	switch in.Outcome {
	case "synced":
		if in.GLPITicketID == nil || in.GLPITicketHref == nil {
			return ErrInvalidState
		}
		action = ActionReconciledSynced
		const query = `UPDATE support_requests
			SET sync_state = 'synced', last_error_code = '',
				glpi_ticket_id = ?, glpi_ticket_href = ?,
				synced_at = ?, processed_at = ?, updated_at = ?
			WHERE id = ? AND tenant_id = ? AND sync_state = 'unknown'`

		res, err := tx.ExecContext(ctx, query,
			nullString(in.GLPITicketID), nullString(in.GLPITicketHref),
			now, now, now,
			in.ID, in.TenantID,
		)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrStateConflict
		}

	case "safe_to_retry":
		action = ActionReconciledRetryable
		const query = `UPDATE support_requests
			SET sync_state = 'retryable_error', last_error_code = '',
				processed_at = ?, updated_at = ?
			WHERE id = ? AND tenant_id = ? AND sync_state = 'unknown'`

		res, err := tx.ExecContext(ctx, query, now, now, in.ID, in.TenantID)
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return ErrStateConflict
		}

	default:
		return ErrInvalidState
	}

	eventID := newEventID()
	const eventQuery = `INSERT INTO support_request_events (
		id, support_request_id, tenant_id, actor_type, actor_user_id,
		action, previous_device_id, device_binding_id, result_code, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	actorUserID := nullStringPtr(in.ActorUserID)
	if _, err := tx.ExecContext(ctx, eventQuery,
		eventID, in.ID, in.TenantID, string(ActorTypeUser), nullString(actorUserID),
		string(action), nil, nil, "", now,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *supportStore) ListEvents(ctx context.Context, tenantID, supportRequestID string) ([]*SupportRequestEvent, error) {
	const query = `SELECT id, support_request_id, tenant_id, actor_type, actor_user_id,
		action, previous_device_id, device_binding_id, result_code, created_at
	FROM support_request_events
	WHERE tenant_id = ? AND support_request_id = ?
	ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, tenantID, supportRequestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*SupportRequestEvent
	for rows.Next() {
		var ev SupportRequestEvent
		var actorUser, prevDev, devBinding sql.NullString
		if err := rows.Scan(
			&ev.ID, &ev.SupportRequestID, &ev.TenantID, &ev.ActorType, &actorUser,
			&ev.Action, &prevDev, &devBinding, &ev.ResultCode, &ev.CreatedAt,
		); err != nil {
			return nil, err
		}
		ev.ActorUserID = stringPtr(actorUser)
		ev.PreviousDeviceID = stringPtr(prevDev)
		ev.DeviceBindingID = stringPtr(devBinding)
		events = append(events, &ev)
	}
	return events, rows.Err()
}

func (s *supportStore) scanRow(row *sql.Row) (*SupportRequest, error) {
	req, err := s.scanCurrent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSupportRequestNotFound
	}
	return req, err
}

func (s *supportStore) scanCurrent(scan func(dest ...any) error) (*SupportRequest, error) {
	var req SupportRequest
	var (
		devBindingID, ticketDevBindingID, ticketHostInformed, ticketHostNorm, ticketGLPICompID sql.NullString
		catID, locID, glpiTicketID, glpiTicketHref                                             sql.NullString
	)

	err := scan(
		&req.ID, &req.OwnerID, &req.TenantID, &req.SessionID, &req.ChatJID,
		&devBindingID, &req.HostnameInformed, &req.HostnameNormalized,
		&ticketDevBindingID, &ticketHostInformed, &ticketHostNorm, &ticketGLPICompID,
		&req.RequesterName, &req.Title, &req.Description, &catID, &locID, &req.Priority,
		&glpiTicketID, &glpiTicketHref,
		&req.ExternalID, &req.IdempotencyKey, &req.PayloadFingerprint,
		&req.SyncState, &req.LastErrorCode, &req.AttemptCount,
		&req.ProcessingToken, &req.ProcessingStartedAt, &req.ProcessedAt,
		&req.CreatedAt, &req.UpdatedAt, &req.SyncedAt,
	)
	if err != nil {
		return nil, err
	}

	req.DeviceBindingID = stringPtr(devBindingID)
	req.TicketDeviceBindingID = stringPtr(ticketDevBindingID)
	req.TicketHostnameInformed = stringPtr(ticketHostInformed)
	req.TicketHostnameNormalized = stringPtr(ticketHostNorm)
	req.TicketGLPIComputerID = stringPtr(ticketGLPICompID)
	req.CategoryID = stringPtr(catID)
	req.LocationID = stringPtr(locID)
	req.GLPITicketID = stringPtr(glpiTicketID)
	req.GLPITicketHref = stringPtr(glpiTicketHref)

	return &req, nil
}

func nullString(s *string) sql.NullString {
	if s == nil || strings.TrimSpace(*s) == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: strings.TrimSpace(*s), Valid: true}
}

func stringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullStringPtr(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

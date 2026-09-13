package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// SupportRequestPublicDTO is the sanitized, public representation of a support request.
// It deliberately omits internal security tokens (processing_token, payload_fingerprint).
type SupportRequestPublicDTO struct {
	ID                 string  `json:"id"`
	SessionID          string  `json:"sessionId"`
	ChatJID            string  `json:"chatJid"`
	Title              string  `json:"title"`
	Description        string  `json:"description"`
	RequesterName      string  `json:"requesterName"`
	HostnameInformed   string  `json:"hostnameInformed"`
	HostnameNormalized string  `json:"hostnameNormalized"`
	DeviceBindingID    *string `json:"deviceBindingId,omitempty"`
	SyncState          string  `json:"syncState"`
	LastErrorCode      *string `json:"lastErrorCode,omitempty"`
	AttemptCount       int     `json:"attemptCount"`
	GLPITicketID       *string `json:"glpiTicketId,omitempty"`
	GLPITicketHref     *string `json:"glpiTicketHref,omitempty"`
	CategoryID         *string `json:"categoryId,omitempty"`
	LocationID         *string `json:"locationId,omitempty"`
	Priority           int     `json:"priority"`
	CreatedAt          int64   `json:"createdAt"`
	UpdatedAt          int64   `json:"updatedAt"`
	ProcessedAt        *int64  `json:"processedAt,omitempty"`
}

func toPublicSupportRequestDTO(r *SupportRequest) SupportRequestPublicDTO {
	if r == nil {
		return SupportRequestPublicDTO{}
	}
	dto := SupportRequestPublicDTO{
		ID:                 r.ID,
		SessionID:          r.SessionID,
		ChatJID:            r.ChatJID,
		Title:              r.Title,
		Description:        r.Description,
		RequesterName:      r.RequesterName,
		HostnameInformed:   r.HostnameInformed,
		HostnameNormalized: r.HostnameNormalized,
		DeviceBindingID:    r.DeviceBindingID,
		SyncState:          string(r.SyncState),
		AttemptCount:       r.AttemptCount,
		GLPITicketID:       r.GLPITicketID,
		GLPITicketHref:     r.GLPITicketHref,
		CategoryID:         r.CategoryID,
		LocationID:         r.LocationID,
		Priority:           r.Priority,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
	if r.LastErrorCode != "" {
		dto.LastErrorCode = &r.LastErrorCode
	}
	if r.ProcessedAt > 0 {
		dto.ProcessedAt = &r.ProcessedAt
	}
	return dto
}

// DeviceBindingPublicDTO is the sanitized representation of a device binding.
type DeviceBindingPublicDTO struct {
	ID                 string `json:"id"`
	Hostname           string `json:"hostname"`
	HostnameNormalized string `json:"hostnameNormalized"`
	GLPIComputerID     string `json:"glpiComputerId,omitempty"`
	TacticalAgentID    string `json:"tacticalAgentId,omitempty"`
	TacticalClientID   string `json:"tacticalClientId,omitempty"`
	TacticalSiteID     string `json:"tacticalSiteId,omitempty"`
	SectorCode         string `json:"sectorCode,omitempty"`
	Patrimonio         string `json:"patrimonio,omitempty"`
	MatchStatus        string `json:"matchStatus"`
	LastVerifiedAt     int64  `json:"lastVerifiedAt"`
	CreatedAt          int64  `json:"createdAt"`
	UpdatedAt          int64  `json:"updatedAt"`
}

func toPublicDeviceBindingDTO(b DeviceBinding) DeviceBindingPublicDTO {
	return DeviceBindingPublicDTO{
		ID:                 b.ID,
		Hostname:           b.Hostname,
		HostnameNormalized: b.HostnameNormalized,
		GLPIComputerID:     b.GLPIComputerID,
		TacticalAgentID:    b.TacticalAgentID,
		TacticalClientID:   b.TacticalClientID,
		TacticalSiteID:     b.TacticalSiteID,
		SectorCode:         b.SectorCode,
		Patrimonio:         b.Patrimonio,
		MatchStatus:        b.MatchStatus,
		LastVerifiedAt:     b.LastVerifiedAt,
		CreatedAt:          b.CreatedAt,
		UpdatedAt:          b.UpdatedAt,
	}
}

// CreateSupportTicketReq represents the body expected by POST .../support/ticket.
type CreateSupportTicketReq struct {
	RequesterName   string  `json:"requesterName"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	DeviceBindingID *string `json:"deviceBindingId,omitempty"`
	Hostname        *string `json:"hostname,omitempty"`
	CategoryID      *string `json:"categoryId,omitempty"`
	LocationID      *string `json:"locationId,omitempty"`
	Priority        int     `json:"priority,omitempty"`
}

// UpdateDeviceReq represents the body expected by PUT /api/support/requests/{id}/device.
type UpdateDeviceReq struct {
	DeviceBindingID string `json:"deviceBindingId"`
}

// ReconcileReq represents the body expected by POST /api/support/requests/{id}/reconcile.
type ReconcileReq struct {
	Outcome      string  `json:"outcome"`
	GLPITicketID *string `json:"glpiTicketId,omitempty"`
}

// SupportTicketResponseEnvelope is the standard success envelope for ticket operations.
type SupportTicketResponseEnvelope struct {
	SupportRequest     SupportRequestPublicDTO `json:"supportRequest"`
	Device             *DeviceBindingPublicDTO `json:"device"`
	GLPIComputer       any                     `json:"glpiComputer"`
	TacticalAgent      any                     `json:"tacticalAgent"`
	Warnings           []string                `json:"warnings"`
	GLPIContextUpdated *bool                   `json:"glpiContextUpdated,omitempty"`
}

// ConversationSupportPanelResponse represents the conversation support panel payload.
type ConversationSupportPanelResponse struct {
	SupportRequests       []SupportRequestPublicDTO `json:"supportRequests"`
	CurrentSupportRequest *SupportRequestPublicDTO  `json:"currentSupportRequest"`
}

// SupportErrorDetail represents the public error detail structure.
type SupportErrorDetail struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds,omitempty"`
}

// SupportErrorEnvelope represents the public error JSON envelope.
type SupportErrorEnvelope struct {
	Error SupportErrorDetail `json:"error"`
}

func writeSupportError(w http.ResponseWriter, status int, code, msg string, retryAfterSeconds int) {
	w.Header().Set("Content-Type", "application/json")
	if retryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(SupportErrorEnvelope{
		Error: SupportErrorDetail{
			Code:              code,
			Message:           msg,
			RetryAfterSeconds: retryAfterSeconds,
		},
	})
}

var (
	errUnsupportedMediaType = errors.New("unsupported media type")
	errInvalidRequest       = errors.New("invalid request body")
)

func enforceJSONContentType(r *http.Request) error {
	ct := r.Header.Get("Content-Type")
	if strings.TrimSpace(ct) == "" {
		return errUnsupportedMediaType
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return errUnsupportedMediaType
	}
	return nil
}

type countedReader struct {
	r io.Reader
	n int64
}

func (c *countedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func decodeJSONStrict(r *http.Request, maxBytes int64, dst any) error {
	if r.Body == nil {
		return errInvalidRequest
	}
	limited := io.LimitReader(r.Body, maxBytes+1)
	cr := &countedReader{r: limited}
	dec := json.NewDecoder(cr)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errInvalidRequest
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return errInvalidRequest
	}
	if cr.n > maxBytes {
		return errInvalidRequest
	}
	var probe [1]byte
	pn, _ := r.Body.Read(probe[:])
	if pn > 0 {
		return errInvalidRequest
	}
	return nil
}

func ensureNoBody(r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	var buf [1]byte
	n, _ := r.Body.Read(buf[:])
	return n == 0
}

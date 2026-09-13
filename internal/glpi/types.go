package glpi

import (
	"net/http"
	"time"
)

const DefaultTimeout = 20 * time.Second

// Config contains only operator-provided integration configuration. Secrets are
// retained in memory and never included in errors.
type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	Username     string
	Password     string

	EntityID        string
	ProfileID       string
	EntityRecursive *bool
	AcceptLanguage  string

	Timeout    time.Duration
	HTTPClient *http.Client
}

// Reference is the stable subset used for GLPI dropdown-like objects.
type Reference struct {
	ID   string
	Name string
}

// Computer is the stable subset consumed by the support domain.
type Computer struct {
	ID           string
	Name         string
	SerialNumber string
	Entity       *Reference
}

// IDReference represents writable GLPI relations whose API input is {"id": N}.
type IDReference struct {
	ID int64 `json:"id"`
}

// EntityReference represents the writable entity form exposed by Ticket.
type EntityReference struct {
	CompleteName string `json:"completename"`
}

// TicketInput is serialized directly as the High-Level API Ticket schema. It is
// intentionally not wrapped in the legacy {"input": ...} envelope.
type TicketInput struct {
	Name          string           `json:"name"`
	Content       string           `json:"content"`
	Type          int              `json:"type,omitempty"`
	Urgency       int              `json:"urgency,omitempty"`
	Impact        int              `json:"impact,omitempty"`
	Priority      int              `json:"priority,omitempty"`
	Entity        *EntityReference `json:"entity,omitempty"`
	Location      *IDReference     `json:"location,omitempty"`
	Category      *IDReference     `json:"category,omitempty"`
	RequestType   *IDReference     `json:"request_type,omitempty"`
	UserRecipient *IDReference     `json:"user_recipient,omitempty"`
	ExternalID    string           `json:"external_id,omitempty"`
}

// CreatedTicket is the 201 response returned by the High-Level API.
type CreatedTicket struct {
	ID   string
	Href string
}

// Ticket represents a GLPI ticket fetched via the High-Level API.
type Ticket struct {
	ID         string
	Href       string
	ExternalID string
}

package main

import (
	"fmt"
	"html"
	"strings"
)

// FormatTicketContent constructs the safe HTML-escaped content body for the GLPI ticket.
// Each user-provided or equipment-derived string is passed through html.EscapeString individually
// before newlines are converted to <br>, preventing HTML injection / XSS attacks.
func FormatTicketContent(req *SupportRequest) string {
	escapedDesc := html.EscapeString(req.Description)
	escapedDesc = strings.ReplaceAll(escapedDesc, "\r\n", "<br>")
	escapedDesc = strings.ReplaceAll(escapedDesc, "\r", "<br>")
	escapedDesc = strings.ReplaceAll(escapedDesc, "\n", "<br>")

	hostInformed := "não informado"
	if req.TicketHostnameInformed != nil && strings.TrimSpace(*req.TicketHostnameInformed) != "" {
		hostInformed = html.EscapeString(strings.TrimSpace(*req.TicketHostnameInformed))
	}

	compID := "não confirmado"
	if req.TicketGLPIComputerID != nil && strings.TrimSpace(*req.TicketGLPIComputerID) != "" {
		compID = html.EscapeString(strings.TrimSpace(*req.TicketGLPIComputerID))
	}

	return fmt.Sprintf(
		"Descrição: %s<br><br>"+
			"Equipamento informado: %s<br>"+
			"GLPI Computer ID: %s<br>"+
			"Vínculo nativo: indisponível na API v2.3; contexto textual<br>"+
			"Origem: WACalls<br>"+
			"Referência: %s",
		escapedDesc,
		hostInformed,
		compID,
		req.ExternalID,
	)
}

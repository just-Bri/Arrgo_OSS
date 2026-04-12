package handlers

import "Arrgo/services"

// Handlers holds the services that need to be injected into HTTP handlers.
// Handlers that only interact with the database or config remain package-level
// functions; those that call a service are methods on this struct.
type Handlers struct {
	Metadata   *services.MetadataService
	Subtitle   *services.SubtitleService
	Automation *services.AutomationService // may be nil if qBittorrent is unavailable
}

func NewHandlers(m *services.MetadataService, s *services.SubtitleService, a *services.AutomationService) *Handlers {
	return &Handlers{
		Metadata:   m,
		Subtitle:   s,
		Automation: a,
	}
}

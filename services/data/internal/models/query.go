package models

type QueryRequest struct {
	Statement       string                 `json:"statement"`
	Parameters      []interface{}          `json:"parameters,omitempty"`
	NamedParameters map[string]interface{} `json:"named_parameters,omitempty"`
}

type QueryResponse struct {
	Results  []interface{}  `json:"results"`
	Metadata *QueryMetadata `json:"metadata,omitempty"`
}

type QueryMetadata struct {
	RequestID       string      `json:"request_id"`
	ClientContextID string      `json:"client_context_id"`
	Status          string      `json:"status"`
	Metrics         interface{} `json:"metrics,omitempty"`
}

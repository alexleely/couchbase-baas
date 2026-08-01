package models

import "time"

type OAuthState struct {
	State        string    `json:"state"`
	Provider     string    `json:"provider"`
	CodeVerifier string    `json:"code_verifier"`
	CreatedAt    time.Time `json:"created_at"`
}

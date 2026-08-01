package models

import "time"

type SAMLProvider struct {
	Domain        string    `json:"domain"` // document key (e.g., acme.com)
	IdPEntityID   string    `json:"idp_entity_id"`
	IdPSSOURL     string    `json:"idp_sso_url"`
	IdPPublicCert string    `json:"idp_public_cert"` // X.509 Certificate string
	CreatedAt     time.Time `json:"created_at"`
}

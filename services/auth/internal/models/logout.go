package models

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

package models

type BucketConfig struct {
	ID     string `json:"id"`     // Bucket name
	Public bool   `json:"public"` // True if publicly readable
}

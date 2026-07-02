package models

import (
	"github.com/google/uuid"
)

// PushSubscription stores a Web Push (VAPID) subscription for one user
// device/browser, used to notify agents of new messages when no tab or
// installed PWA is open (e.g. Android with the app closed).
type PushSubscription struct {
	BaseModel
	OrganizationID uuid.UUID `gorm:"type:uuid;index;not null" json:"organization_id"`
	UserID         uuid.UUID `gorm:"type:uuid;index;not null" json:"user_id"`
	Endpoint       string    `gorm:"type:text;uniqueIndex;not null" json:"endpoint"`
	P256dh         string    `gorm:"size:255;not null" json:"-"`
	Auth           string    `gorm:"size:255;not null" json:"-"`
	UserAgent      string    `gorm:"size:512" json:"user_agent"`
}

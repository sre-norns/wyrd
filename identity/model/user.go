package model

// User is persisted identity data; services expose PersonalProfile instead.
type User struct {
	LastModifiedBy ResourceActor `gorm:"serializer:json;type:jsonb"`
	ID             string        `gorm:"primaryKey"`
	Email          string        `gorm:"uniqueIndex"`
	DisplayName    *string
	Password       []byte
	SystemAdmin    bool
	Status         string
	Revision       int64 `gorm:"not null;default:1"`
}

func (User) TableName() string { return "users" }

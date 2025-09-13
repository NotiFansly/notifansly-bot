package models

import (
	"time"
)

type MonitoredUser struct {
	GuildID                 string `gorm:"primaryKey;column:guild_id"`
	UserID                  string `gorm:"primaryKey;column:user_id"`
	Username                string `gorm:"column:username"`
	NotificationChannel     string `gorm:"column:notification_channel"`
	PostNotificationChannel string `gorm:"column:post_notification_channel"`
	LiveNotificationChannel string `gorm:"column:live_notification_channel"`
	LastPostID              string `gorm:"column:last_post_id"`
	LastStreamStart         int64  `gorm:"column:last_stream_start"`
	MentionRole             string `gorm:"column:mention_role"`
	AvatarLocation          string `gorm:"column:avatar_location"`
	AvatarLocationUpdatedAt int64  `gorm:"column:avatar_location_updated_at"`
	LiveImageURL            string `gorm:"column:live_image_url"`
	PostsEnabled            bool   `gorm:"column:posts_enabled"`
	LiveEnabled             bool   `gorm:"column:live_enabled"`
	LiveMentionRole         string `gorm:"column:live_mention_role"`
	PostMentionRole         string `gorm:"column:post_mention_role"`
}

type GuildSubscription struct {
	GuildID          string `gorm:"primaryKey;column:guild_id"`
	SubscriptionTier string `gorm:"column:subscription_tier"` // e.g., "tier1", "tier2", "enterprise"
	UserLimit        int    `gorm:"column:user_limit"`
	ExpiresAt        int64  `gorm:"column:expires_at"` // UNIX timestamp for expiration
	UpdatedAt        int64  `gorm:"autoUpdateTime"`
}

type ServiceStatus struct {
	ServiceName   string    `gorm:"primaryKey;column:service_name"`
	Status        string    `gorm:"column:status"`
	LastHeartbeat time.Time `gorm:"column:last_heartbeat"`
	Details       string    `gorm:"column:details"`
}

func (ServiceStatus) TableName() string {
	return "service_status"
}

// SystemStat holds key-value pairs for system-wide statistics.
type SystemStat struct {
	StatKey   string    `gorm:"primaryKey;column:stat_key"`
	StatValue int64     `gorm:"column:stat_value"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (SystemStat) TableName() string {
	return "system_stats"
}

func (GuildSubscription) TableName() string {
	return "guild_subscriptions"
}

type SchemaVersion struct {
	Version int `gorm:"primaryKey"`
}

func (MonitoredUser) TableName() string {
	return "monitored_users"
}

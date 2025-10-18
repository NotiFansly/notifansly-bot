package database

import (
	"errors"
	"strings"
	"time"

	"github.com/NotiFansly/notifansly-bot/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository handles database operations for monitored users
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new repository instance
func NewRepository() *Repository {
	return &Repository{db: DB}
}

// UpsertNotificationFormats creates or updates the custom message formats for a user.
func (r *Repository) UpsertNotificationFormats(formats *models.UserNotificationFormat) error {
	return WithRetry(func() error {
		return r.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "guild_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"post_message_format", "live_message_format"}),
		}).Create(formats).Error
	})
}

// GetNotificationFormats retrieves custom message formats for a user.
// Returns (nil, nil) if no record is found, which is not an error.
func (r *Repository) GetNotificationFormats(guildID, userID string) (*models.UserNotificationFormat, error) {
	var formats models.UserNotificationFormat
	err := WithRetry(func() error {
		result := r.db.Where("guild_id = ? AND user_id = ?", guildID, userID).First(&formats)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil // Not found is not an error
		}
		return result.Error
	})
	if err != nil || formats.GuildID == "" {
		return nil, err // Return nil if error or record is empty after retries
	}
	return &formats, nil
}

// GetNotificationFormatsForUser fetches all format settings for a given user ID across all guilds.
func (r *Repository) GetNotificationFormatsForUser(userID string) (map[string]models.UserNotificationFormat, error) {
	var results []models.UserNotificationFormat
	err := WithRetry(func() error {
		return r.db.Where("user_id = ?", userID).Find(&results).Error
	})
	if err != nil {
		return nil, err
	}

	formatMap := make(map[string]models.UserNotificationFormat)
	for _, r := range results {
		formatMap[r.GuildID] = r
	}
	return formatMap, nil
}

func (r *Repository) UpsertEmbedColors(colors *models.UserEmbedColor) error {
	return WithRetry(func() error {
		return r.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "guild_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"post_embed_color", "live_embed_color"}),
		}).Create(colors).Error
	})
}

// GetEmbedColors retrieves the custom embed colors for a specific user in a guild.
// It returns (nil, nil) if no record is found, which is not an error.
func (r *Repository) GetEmbedColors(guildID, userID string) (*models.UserEmbedColor, error) {
	var colors models.UserEmbedColor
	err := WithRetry(func() error {
		result := r.db.Where("guild_id = ? AND user_id = ?", guildID, userID).First(&colors)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil // Not found is not an error, we'll return a nil pointer
		}
		return result.Error
	})

	if err != nil {
		return nil, err
	}
	// Check if the record was actually found after retries
	if colors.GuildID == "" {
		return nil, nil
	}
	return &colors, nil
}

// GetEmbedColorsForUser fetches all custom embed color settings for a given user ID.
// It returns a map where the key is the GuildID.
func (r *Repository) GetEmbedColorsForUser(userID string) (map[string]models.UserEmbedColor, error) {
	var results []models.UserEmbedColor
	err := WithRetry(func() error {
		return r.db.Where("user_id = ?", userID).Find(&results).Error
	})
	if err != nil {
		return nil, err
	}

	colorMap := make(map[string]models.UserEmbedColor)
	for _, r := range results {
		colorMap[r.GuildID] = r
	}
	return colorMap, nil
}

func (r *Repository) UpsertServiceStatus(status *models.ServiceStatus) error {
	return WithRetry(func() error {
		// GORM's Save works as an upsert for records with a primary key.
		return r.db.Save(status).Error
	})
}

// IncrementNotificationCount atomically increments the total notification count.
func (r *Repository) IncrementPostCount() error {
	return WithRetry(func() error {
		return r.db.Model(&models.SystemStat{}).
			Where("stat_key = ?", "total_posts_sent").
			Updates(map[string]any{
				"stat_value": gorm.Expr("stat_value + 1"),
				"updated_at": time.Now(),
			}).Error
	})
}

// IncrementLiveCount atomically increments the total live stream notification count.
func (r *Repository) IncrementLiveCount() error {
	return WithRetry(func() error {
		return r.db.Model(&models.SystemStat{}).
			Where("stat_key = ?", "total_live_sent").
			Updates(map[string]any{
				"stat_value": gorm.Expr("stat_value + 1"),
				"updated_at": time.Now(),
			}).Error
	})
}

// --- ADD THIS NEW METHOD for API Health ---

func (r *Repository) UpdateAPIHealthBulk(serviceName string, totalToAdd, successfulToAdd uint64) error {
	if totalToAdd == 0 && successfulToAdd == 0 {
		return nil
	}

	return WithRetry(func() error {
		updates := map[string]interface{}{
			"total_requests":      gorm.Expr("total_requests + ?", totalToAdd),
			"successful_requests": gorm.Expr("successful_requests + ?", successfulToAdd),
		}
		return r.db.Model(&models.APIHealthStat{}).
			Where("service_name = ?", serviceName).
			Updates(updates).Error
	})
}

// RecordAPIHealth records the outcome of an API call.
func (r *Repository) RecordAPIHealth(serviceName string, success bool) error {
	return WithRetry(func() error {
		updates := map[string]any{
			"total_requests": gorm.Expr("total_requests + 1"),
		}
		if success {
			updates["successful_requests"] = gorm.Expr("successful_requests + 1")
		}
		return r.db.Model(&models.APIHealthStat{}).
			Where("service_name = ?", serviceName).
			Updates(updates).Error
	})
}

// GetMonitoredUsers returns all monitored users
func (r *Repository) GetMonitoredUsers() ([]models.MonitoredUser, error) {
	var users []models.MonitoredUser
	err := WithRetry(func() error {
		return r.db.Find(&users).Error
	})
	return users, err
}

// GetMonitoredUser returns a specific monitored user
func (r *Repository) GetMonitoredUser(guildID, userID string) (*models.MonitoredUser, error) {
	var user models.MonitoredUser
	err := WithRetry(func() error {
		result := r.db.Where("guild_id = ? AND user_id = ?", guildID, userID).First(&user)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		// Pass gorm.ErrRecordNotFound up to be handled by the caller
		return result.Error
	})

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // No user found
	}
	return &user, err
}

func (r *Repository) GetMonitoredUserByUsername(guildID, username string) (*models.MonitoredUser, error) {
	username = strings.ToLower(username)
	var user models.MonitoredUser
	err := WithRetry(func() error {
		result := r.db.Where("guild_id = ? AND LOWER(username) = ?", guildID, username).First(&user)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		return result.Error
	})

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // No user found
	}
	return &user, err
}

// AddMonitoredUser adds a new monitored user
func (r *Repository) AddMonitoredUser(user *models.MonitoredUser) error {
	return WithRetry(func() error {
		return r.db.Create(user).Error
	})
}

func (r *Repository) AddOrUpdateMonitoredUser(user *models.MonitoredUser) error {
	return WithRetry(func() error {
		return r.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "guild_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"username", "notification_channel", "post_notification_channel", "live_notification_channel",
				"last_post_id", "last_stream_start", "mention_role", "avatar_location",
				"avatar_location_updated_at", "live_image_url", "posts_enabled", "live_enabled",
				"live_mention_role", "post_mention_role",
			}),
		}).Create(user).Error
	})
}

// UpdateMonitoredUser updates an existing monitored user
func (r *Repository) UpdateMonitoredUser(user *models.MonitoredUser) error {
	return WithRetry(func() error {
		return r.db.Save(user).Error
	})
}

// DeleteMonitoredUser deletes a monitored user
func (r *Repository) DeleteMonitoredUser(guildID, userID string) error {
	return WithRetry(func() error {
		return r.db.Delete(&models.MonitoredUser{}, "guild_id = ? AND user_id = ?", guildID, userID).Error
	})
}

// MODIFIED: DeleteMonitoredUserByUsername to also clean up formats
func (r *Repository) DeleteMonitoredUserByUsername(guildID, username string) error {
	username = strings.ToLower(username)
	return r.db.Transaction(func(tx *gorm.DB) error {
		// First, find the user to get their ID for deleting related data
		var user models.MonitoredUser
		if err := tx.Where("guild_id = ? AND LOWER(username) = ?", guildID, username).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("user not found")
			}
			return err
		}

		// Delete the format settings
		if err := tx.Where("guild_id = ? AND user_id = ?", guildID, user.UserID).Delete(&models.UserNotificationFormat{}).Error; err != nil {
			return err
		}

		// Delete the embed color settings
		if err := tx.Where("guild_id = ? AND user_id = ?", guildID, user.UserID).Delete(&models.UserEmbedColor{}).Error; err != nil {
			return err
		}

		// Finally, delete the monitored user
		if err := tx.Where("guild_id = ? AND user_id = ?", guildID, user.UserID).Delete(&models.MonitoredUser{}).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *Repository) GetGuildSubscription(guildID string) (*models.GuildSubscription, error) {
	var subscription models.GuildSubscription
	result := r.db.Where("guild_id = ?", guildID).First(&subscription)
	if result.Error != nil {
		return nil, result.Error
	}
	return &subscription, nil
}

// Function to create or update a subscription (will be used by the webhook and owner command)
func (r *Repository) UpsertGuildSubscription(sub *models.GuildSubscription) error {
	// This will either create a new record or update the existing one for the GuildID
	return r.db.Save(sub).Error
}

// GetMonitoredUsersForGuild returns all monitored users for a specific guild
func (r *Repository) GetMonitoredUsersForGuild(guildID string) ([]models.MonitoredUser, error) {
	var users []models.MonitoredUser
	err := WithRetry(func() error {
		return r.db.Where("guild_id = ?", guildID).Find(&users).Error
	})
	return users, err
}

// New function to count monitored users for a guild
func (r *Repository) CountMonitoredUsersForGuild(guildID string) (int64, error) {
	var count int64
	err := WithRetry(func() error {
		return r.db.Model(&models.MonitoredUser{}).Where("guild_id = ?", guildID).Count(&count).Error
	})
	return count, err
}

// UpdateLastPostID updates the last post ID for a monitored user
func (r *Repository) UpdateLastPostID(guildID, userID, postID string) error {
	return WithRetry(func() error {
		return r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND user_id = ?", guildID, userID).
			Update("last_post_id", postID).Error
	})
}

// UpdateLastStreamStart updates the last stream start for a monitored user
func (r *Repository) UpdateLastStreamStart(guildID, userID string, timestamp int64) error {
	return WithRetry(func() error {
		return r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND user_id = ?", guildID, userID).
			Update("last_stream_start", timestamp).Error
	})
}

// UpdateAvatarInfo updates the avatar information for a monitored user
func (r *Repository) UpdateAvatarInfo(guildID, userID, avatarLocation string) error {
	return WithRetry(func() error {
		return r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND user_id = ?", guildID, userID).
			Updates(map[string]any{
				"avatar_location":            avatarLocation,
				"avatar_location_updated_at": time.Now().Unix(),
			}).Error
	})
}

func (r *Repository) UpdateLastPostIDByUsername(guildID, username, postID string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("last_post_id", postID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) UpdateAvatarInfoByUsername(guildID, username, avatarLocation string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Updates(map[string]any{
				"avatar_location":            avatarLocation,
				"avatar_location_updated_at": time.Now().Unix(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) DisablePostsByUsername(guildID, username string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("posts_enabled", false)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) EnablePostsByUsername(guildID, username string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("posts_enabled", true)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) DisableLiveByUsername(guildID, username string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("live_enabled", false)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) EnableLiveByUsername(guildID, username string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("live_enabled", true)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) CountMonitoredUsers() (int64, error) {
	var count int64
	err := WithRetry(func() error {
		return r.db.Model(&models.MonitoredUser{}).Count(&count).Error
	})
	return count, err
}

func (r *Repository) CountGuilds() (int64, error) {
	var count int64
	err := WithRetry(func() error {
		return r.db.Model(&models.MonitoredUser{}).Distinct("guild_id").Count(&count).Error
	})
	return count, err
}

func (r *Repository) DeleteAllUsersInGuild(guildID string) error {
	return WithRetry(func() error {
		return r.db.Delete(&models.MonitoredUser{}, "guild_id = ?", guildID).Error
	})
}

func (r *Repository) UpdateLiveImageURL(guildID, username, imageURL string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("live_image_url", imageURL)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) UpdatePostChannel(guildID, username, channelID string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("post_notification_channel", channelID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) UpdateLiveChannel(guildID, username, channelID string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("live_notification_channel", channelID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) UpdatePostMentionRole(guildID, username, roleID string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("post_mention_role", roleID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

func (r *Repository) UpdateLiveMentionRole(guildID, username, roleID string) error {
	username = strings.ToLower(username)
	return WithRetry(func() error {
		result := r.db.Model(&models.MonitoredUser{}).
			Where("guild_id = ? AND LOWER(username) = ?", guildID, username).
			Update("live_mention_role", roleID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("user not found")
		}
		return nil
	})
}

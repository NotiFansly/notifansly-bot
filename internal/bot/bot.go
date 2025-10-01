package bot

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/NotiFansly/notifansly-bot/api"
	"github.com/NotiFansly/notifansly-bot/internal/config"
	"github.com/NotiFansly/notifansly-bot/internal/database"
	"github.com/NotiFansly/notifansly-bot/internal/embed"
	"github.com/NotiFansly/notifansly-bot/internal/health"
	"github.com/NotiFansly/notifansly-bot/internal/models"
)

type Bot struct {
	Session   *discordgo.Session
	APIClient *api.Client
	Repo      *database.Repository
}

func New(aggregator *health.Aggregator) (*Bot, error) {
	discord, err := discordgo.New("Bot " + config.DiscordToken)
	if err != nil {
		return nil, err
	}

	apiClient, _ := api.NewClient(config.FanslyToken, config.UserAgent, aggregator)

	bot := &Bot{
		Session:   discord,
		APIClient: apiClient,
		Repo:      database.NewRepository(),
	}

	bot.registerHandlers()

	return bot, nil
}

func (b *Bot) Start() error {
	err := b.Session.Open()
	if err != nil {
		return err
	}

	go b.monitorUsers()
	go b.updateStatusPeriodically()
	go b.heartbeat()

	return nil
}

func (b *Bot) Stop() {
	b.Session.Close()
}

func (b *Bot) registerHandlers() {
	b.Session.AddHandler(b.ready)
	b.Session.AddHandler(b.interactionCreate)
	b.Session.AddHandler(b.guildCreate)
	b.Session.AddHandler(b.guildDelete)
}

func (b *Bot) guildCreate(s *discordgo.Session, event *discordgo.GuildCreate) {
	log.Printf("Bot joined a new server: %s", event.Guild.Name)
	b.updateBotStatus()
}

func (b *Bot) guildDelete(s *discordgo.Session, event *discordgo.GuildDelete) {
	if !event.Unavailable {
		log.Printf("Bot removed from guild: %s. Cleaning up associated data.", event.ID)
		err := b.Repo.DeleteAllUsersInGuild(event.ID)
		if err != nil {
			log.Printf("Error deleting users for guild %s: %v", event.ID, err)
		} else {
			log.Printf("Successfully cleaned up data for guild %s", event.ID)
		}
	} else {
		log.Printf("Guild %s became unavailable.", event.ID)
	}

	b.updateBotStatus()
}

func (b *Bot) heartbeat() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		log.Println("Sending heartbeat...")
		status := &models.ServiceStatus{
			ServiceName:   "discord_bot",
			Status:        "operational",
			LastHeartbeat: time.Now(),
		}
		if err := b.Repo.UpsertServiceStatus(status); err != nil {
			log.Printf("Error sending heartbeat: %v", err)
		}
		<-ticker.C
	}
}

func (b *Bot) monitorUsers() {
	ticker := time.NewTicker(time.Duration(config.MonitorIntervalSeconds) * time.Second)
	defer ticker.Stop()

	numWorkers := config.MonitorWorkerCount
	if numWorkers <= 0 {
		numWorkers = 1
	}
	jobs := make(chan []models.MonitoredUser, 100)

	for w := 1; w <= numWorkers; w++ {
		go b.worker(w, jobs)
	}

	log.Println("Dispatching initial monitoring cycle...")
	b.dispatchMonitoringJobs(jobs)

	for range ticker.C {
		b.dispatchMonitoringJobs(jobs)
	}
}

func (b *Bot) dispatchMonitoringJobs(jobs chan<- []models.MonitoredUser) {
	users, err := b.Repo.GetMonitoredUsers()
	if err != nil {
		log.Printf("Error getting monitored users: %v", err)
		return
	}

	userGroups := make(map[string][]models.MonitoredUser)
	for _, user := range users {
		userGroups[user.UserID] = append(userGroups[user.UserID], user)
	}

	log.Printf("Dispatching %d unique users to %d workers.", len(userGroups), config.MonitorWorkerCount)

	for _, userEntries := range userGroups {
		jobs <- userEntries
	}
}

func (b *Bot) worker(id int, jobs <-chan []models.MonitoredUser) {
	avatarRefreshDuration := int64(config.AvatarRefreshIntervalHours * 60 * 60)

	for userEntries := range jobs {
		primaryUser := userEntries[0]

		if time.Now().Unix()-primaryUser.AvatarLocationUpdatedAt > avatarRefreshDuration {
			newAvatarLocation, err := b.refreshAvatarURL(primaryUser.Username)
			if err != nil {
				log.Printf("[Worker %d] Error refreshing avatar URL for %s: %v", id, primaryUser.Username, err)
			} else {
				for _, user := range userEntries {
					err = b.Repo.UpdateAvatarInfo(user.GuildID, user.UserID, newAvatarLocation)
					if err != nil {
						log.Printf("[Worker %d] Error updating avatar URL in DB for %s in guild %s: %v", id, user.Username, user.GuildID, err)
					}
				}
				for i := range userEntries {
					userEntries[i].AvatarLocation = newAvatarLocation
				}
			}
		}

		b.checkUserLiveStreamOptimized(userEntries)
		b.checkUserPostsOptimized(userEntries)
	}
}

// formatNotificationMessage processes the custom message format and ensures a mention role is always included if set.
func (b *Bot) formatNotificationMessage(guildID, userID, username, mentionRole, placeholder string) string {
	// 1. Fetch the custom format from the database
	formats, err := b.Repo.GetNotificationFormats(guildID, userID)
	if err != nil {
		log.Printf("Could not fetch notification formats for user %s in guild %s: %v", userID, guildID, err)
	}

	var customFormat string
	if formats != nil {
		if placeholder == "{postMention}" {
			customFormat = formats.PostMessageFormat
		} else if placeholder == "{liveMention}" {
			customFormat = formats.LiveMessageFormat
		}
	}

	// 2. Prepare the role mention string, if a role is set
	roleMention := ""
	if mentionRole != "" {
		roleMention = fmt.Sprintf("<@&%s>", mentionRole)
	}

	// 3. Process the final message content
	if customFormat != "" {
		// A custom format exists. First, replace the username placeholder.
		processedMessage := strings.Replace(customFormat, "{username}", username, -1)

		// Check if the user manually included the mention placeholder in their format.
		if strings.Contains(customFormat, placeholder) {
			// They did, so we just replace it with the role mention (or an empty string if no role is set).
			return strings.Replace(processedMessage, placeholder, roleMention, -1)
		} else {
			// They did NOT include the placeholder. We should prepend the mention role if it exists.
			if roleMention != "" {
				// Combine the mention and their custom message.
				return fmt.Sprintf("%s %s", roleMention, processedMessage)
			}
			// If no role is set, just return their custom message.
			return processedMessage
		}
	}

	// 4. No custom format was set. Default behavior is to just send the mention role, if any.
	return roleMention
}

func (b *Bot) checkUserLiveStreamOptimized(userEntries []models.MonitoredUser) {
	liveEnabledUsers := make([]models.MonitoredUser, 0)
	for _, user := range userEntries {
		if user.LiveEnabled {
			liveEnabledUsers = append(liveEnabledUsers, user)
		}
	}

	if len(liveEnabledUsers) == 0 {
		return
	}

	primaryUser := liveEnabledUsers[0]
	streamInfo, err := b.APIClient.GetStreamInfo(primaryUser.UserID)
	if err != nil {
		log.Printf("Error fetching stream info for %s: %v", primaryUser.Username, err)
		return
	}

	colorsMap, err := b.Repo.GetEmbedColorsForUser(primaryUser.UserID)
	if err != nil {
		log.Printf("Could not fetch embed colors for user %s: %v", primaryUser.Username, err)
	}

	if streamInfo.Response.Stream.Status == 2 && streamInfo.Response.Stream.StartedAt > primaryUser.LastStreamStart {
		for _, user := range liveEnabledUsers {
			err = b.Repo.UpdateLastStreamStart(user.GuildID, user.UserID, streamInfo.Response.Stream.StartedAt)
			if err != nil {
				log.Printf("Error updating last stream start: %v", err)
				continue
			}

			var embedColor int
			if colorSetting, ok := colorsMap[user.GuildID]; ok {
				embedColor = colorSetting.LiveEmbedColor
			}

			embedMsg := embed.CreateLiveStreamEmbed(user.Username, streamInfo, user.AvatarLocation, user.LiveImageURL, embedColor)

			// --- FIXED THIS LINE ---
			mentionContent := b.formatNotificationMessage(user.GuildID, user.UserID, user.Username, user.LiveMentionRole, "{liveMention}")

			targetChannel := user.LiveNotificationChannel
			if targetChannel == "" {
				targetChannel = user.NotificationChannel
			}

			_, err = b.Session.ChannelMessageSendComplex(targetChannel, &discordgo.MessageSend{
				Content: mentionContent,
				Embed:   embedMsg,
			})
			if err != nil {
				b.logNotificationError("live stream", user, targetChannel, err)
			} else {
				go b.Repo.IncrementLiveCount()
			}
		}
	}
}

func (b *Bot) checkUserPostsOptimized(userEntries []models.MonitoredUser) {
	postEnabledUsers := make([]models.MonitoredUser, 0)
	for _, user := range userEntries {
		if user.PostsEnabled {
			postEnabledUsers = append(postEnabledUsers, user)
		}
	}

	if len(postEnabledUsers) == 0 {
		return
	}

	primaryUser := postEnabledUsers[0]
	latestPosts, err := b.APIClient.GetTimelinePost(primaryUser.UserID)
	if err != nil {
		log.Printf("Error fetching post info for %s: %v", primaryUser.Username, err)
		return
	}

	if len(latestPosts) == 0 {
		return
	}

	latestPost := latestPosts[0]

	colorsMap, err := b.Repo.GetEmbedColorsForUser(primaryUser.UserID)
	if err != nil {
		log.Printf("Could not fetch embed colors for user %s: %v", primaryUser.Username, err)
	}

	for _, user := range postEnabledUsers {
		if latestPost.ID != user.LastPostID {
			err := b.Repo.UpdateLastPostID(user.GuildID, user.UserID, latestPost.ID)
			if err != nil {
				log.Printf("Error updating last post ID for %s in guild %s: %v", user.Username, user.GuildID, err)
				continue
			}

			isFirstPostForThisServer := user.LastPostID == "" || user.LastPostID == "0"

			var embedColor int
			if colorSetting, ok := colorsMap[user.GuildID]; ok {
				embedColor = colorSetting.PostEmbedColor
			}

			embedMsg := embed.CreatePostEmbed(user.Username, latestPost, user.AvatarLocation, nil, embedColor)

			// --- FIXED THIS LINE ---
			mentionContent := b.formatNotificationMessage(user.GuildID, user.UserID, user.Username, user.PostMentionRole, "{postMention}")

			targetChannel := user.PostNotificationChannel
			if targetChannel == "" {
				targetChannel = user.NotificationChannel
			}

			log.Printf("Sending post notification for %s to guild %s. First post: %t", user.Username, user.GuildID, isFirstPostForThisServer)

			_, err = b.Session.ChannelMessageSendComplex(targetChannel, &discordgo.MessageSend{
				Content: mentionContent,
				Embed:   embedMsg,
			})
			if err != nil {
				b.logNotificationError("post", user, targetChannel, err)
			} else {
				go b.Repo.IncrementPostCount()
			}
		}
	}
}

func (b *Bot) logNotificationError(notificationType string, user models.MonitoredUser, targetChannel string, err error) {
	guild, _ := b.Session.Guild(user.GuildID)
	guildName := "Unknown Server"
	if guild != nil {
		guildName = guild.Name
	}
	channel, _ := b.Session.Channel(targetChannel)
	channelName := "Unknown Channel"
	if channel != nil {
		channelName = channel.Name
	}
	log.Printf("Error sending %s notification for %s | Server: %s (%s) | Channel: %s (%s) | Error: %v",
		notificationType,
		user.Username,
		guildName,
		user.GuildID,
		channelName,
		targetChannel,
		err,
	)
}

func (b *Bot) updateStatusPeriodically() {
	ticker := time.NewTicker(time.Duration(config.StatusUpdateIntervalMinutes) * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.updateBotStatus()
	}
}

func (b *Bot) refreshAvatarURL(username string) (string, error) {
	accountInfo, err := b.APIClient.GetAccountInfo(username)
	if err != nil {
		return "", err
	}

	if accountInfo == nil || accountInfo.Avatar.Locations == nil || len(accountInfo.Avatar.Variants) == 0 || len(accountInfo.Avatar.Variants[0].Locations) == 0 {
		return "", fmt.Errorf("invalid account info structure for user %s", username)
	}

	return accountInfo.Avatar.Variants[0].Locations[0].Location, nil
}

func (b *Bot) updateBotStatus() {
	serverCount := len(b.Session.State.Guilds)
	status := fmt.Sprintf("Watching %d servers", serverCount)
	err := b.Session.UpdateStatusComplex(discordgo.UpdateStatusData{
		Activities: []*discordgo.Activity{
			{
				Name: status,
				Type: discordgo.ActivityTypeWatching,
			},
		},
	})
	if err != nil {
		log.Printf("Error updating status: %v", err)
	}
}

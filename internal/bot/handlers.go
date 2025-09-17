package bot

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/fvckgrimm/discord-fansly-notify/internal/config"
	"github.com/fvckgrimm/discord-fansly-notify/internal/database"
	"github.com/fvckgrimm/discord-fansly-notify/internal/models"
)

var (
	tokenRegex     = regexp.MustCompile(`[A-Za-z0-9]{40,}`)
	fanslyURLRegex = regexp.MustCompile(`(?:https?://)?(?:www\.)?(?:fans\.ly|fansly\.com)/([^/\s]+)(?:/.*)?`)
	hexColorRegex  = regexp.MustCompile(`^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$`)
)

func (b *Bot) ready(s *discordgo.Session, event *discordgo.Ready) {
	log.Println("Bot is ready")
	b.registerCommands()
	b.updateBotStatus()
}

func (b *Bot) isBotOwner(i *discordgo.InteractionCreate) bool {
	if config.BotOwnerID == "" {
		return false // Can't be the owner if the ID isn't configured
	}
	return i.Member.User.ID == config.BotOwnerID
}

func (b *Bot) interactionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		// First, handle owner-only command checks
		switch i.ApplicationCommandData().Name {
		case "leave", "servers":
			if !b.isBotOwner(i) {
				b.respondToInteraction(s, i, "This command is for the bot owner only.", true)
				return
			}
		}

		// Second, handle general permission checks for non-owners
		if !b.isBotOwner(i) && !b.hasAdminOrModPermissions(s, i) {
			username := "User"
			if i.User != nil {
				username = i.User.Username
			} else if i.Member != nil && i.Member.User != nil {
				username = i.Member.User.Username
			}
			b.respondToInteraction(s, i, "You do not have permission to use this command.", true)
			log.Printf("Permission denied for user %s", username)
			return
		}

		// Finally, handle the command logic
		switch i.ApplicationCommandData().Name {
		case "add":
			b.handleAddCommand(s, i)
		case "remove":
			b.handleRemoveCommand(s, i)
		case "list":
			b.handleListCommand(s, i)
		case "setliveimage":
			b.handleSetLiveImageCommand(s, i)
		case "toggle":
			b.handleToggleCommand(s, i)
		case "setchannel":
			b.handleSetChannelCommand(s, i)
		case "setpostmention":
			b.handleSetPostMentionCommand(s, i)
		case "setlivemention":
			b.handleSetLiveMentionCommand(s, i)
		case "setcolor":
			b.handleSetColorCommand(s, i)
		case "servers":
			b.handleServersCommand(s, i)
		case "leave":
			b.handleLeaveCommand(s, i)
		case "setlimit":
			b.handleSetLimitCommand(s, i)
		case "setformat":
			b.handleSetFormatCommand(s, i)
		}

	case discordgo.InteractionMessageComponent:
		// All button clicks and other components fall here.
	case discordgo.InteractionModalSubmit:
		// Handle submissions from our new modal
		if strings.HasPrefix(i.ModalSubmitData().CustomID, "format_modal_") {
			b.handleFormatModalSubmit(s, i)
		}
	}
}

func extractUsernameFromURL(input string) string {
	matches := fanslyURLRegex.FindStringSubmatch(input)
	if len(matches) > 1 {
		return matches[1]
	}

	if len(input) > 0 && input[0] == '@' {
		return input[1:]
	}

	return input
}

func (b *Bot) handleAddCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	rawUsername := options[0].StringValue()

	username := extractUsernameFromURL(rawUsername)

	guildLimit := config.MaxMonitoredUsersPerGuild
	subscription, err := b.Repo.GetGuildSubscription(i.GuildID)
	if err == nil && subscription != nil {
		if time.Now().Unix() < subscription.ExpiresAt {
			guildLimit = subscription.UserLimit
		}
	}

	if guildLimit > 0 {
		count, err := b.Repo.CountMonitoredUsersForGuild(i.GuildID)
		if err != nil {
			log.Printf("Error checking guild limit for guild %s: %v", i.GuildID, err)
			b.respondToInteraction(s, i, "An error occurred while checking the server's limit. Please try again later.", true)
			return
		}

		if count >= int64(guildLimit) {
			existingUser, _ := b.Repo.GetMonitoredUserByUsername(i.GuildID, username)
			if existingUser == nil {
				message := fmt.Sprintf(
					"This server is at its limit of %d monitored creators. If your subscription has expired, you can manage your plan here: <https://notifansly.xyz/dashboard/server/%s/billing>",
					guildLimit,
					i.GuildID,
				)
				b.respondToInteraction(s, i, message, true)
				return
			}
		}
	}

	if tokenRegex.MatchString(username) {
		b.respondToInteraction(s, i, "Error: Username appears to contain a token. Please provide a valid username.", true)
		return
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	go func() {
		channel := options[1].ChannelValue(s)
		var mentionRole string
		if len(options) > 2 {
			if role := options[2].RoleValue(s, i.GuildID); role != nil {
				mentionRole = role.ID
			}
		}

		if config.LogChannelID != "" {
			var guildName string
			guild, err := s.Guild(i.GuildID)
			if err != nil {
				log.Printf("Could not retrieve guild details for ID %s: %v", i.GuildID, err)
				guildName = "Unknown Server"
			} else {
				guildName = guild.Name
			}

			logMessage := fmt.Sprintf(
				"`[%s]` User <@%s> (`%s`) added a creator:\n**Creator:** `%s`\n**Server:** %s (`%s`)",
				time.Now().Format("2006-01-02 15:04:05"),
				i.Member.User.ID,
				i.Member.User.Username,
				username,
				guildName,
				i.GuildID,
			)
			_, logErr := s.ChannelMessageSend(config.LogChannelID, logMessage)
			if logErr != nil {
				log.Printf("Failed to send log message to channel %s: %v", config.LogChannelID, logErr)
			}
		}

		accountInfo, err := b.APIClient.GetAccountInfo(username)
		if err != nil {
			log.Printf("Error getting account info for %s: %v", username, err)
			b.editInteractionResponse(s, i, fmt.Sprintf("Error fetching account info: The user might not exist or Fansly API is unavailable. (%v)", err))
			return
		}

		if accountInfo == nil {
			log.Printf("Invalid account info structure for %s", username)
			b.editInteractionResponse(s, i, "Error: Could not retrieve valid account info for this user.")
			return
		}

		var avatarLocation string
		if len(accountInfo.Avatar.Variants) > 0 && len(accountInfo.Avatar.Variants[0].Locations) > 0 {
			avatarLocation = accountInfo.Avatar.Variants[0].Locations[0].Location
		} else {
			log.Printf("Warning: No avatar found for user %s", username)
		}

		timelinePosts, timelineErr := b.APIClient.GetTimelinePost(accountInfo.ID)
		timelineAccessible := timelineErr == nil && len(timelinePosts) >= 0

		if !timelineAccessible {
			if myAccount, err := b.APIClient.GetMyAccountInfo(); err == nil && myAccount.ID != "" {
				if following, err := b.APIClient.GetFollowing(myAccount.ID); err == nil {
					isFollowing := false
					for _, f := range following {
						if f.AccountID == accountInfo.ID {
							isFollowing = true
							break
						}
					}
					if !isFollowing {
						if followErr := b.APIClient.FollowAccount(accountInfo.ID); followErr != nil {
							log.Printf("Note: Could not automatically follow %s: %v", username, followErr)
						}
					}
				}
			}
			timelinePosts, timelineErr = b.APIClient.GetTimelinePost(accountInfo.ID)
			timelineAccessible = timelineErr == nil
		}

		if !timelineAccessible {
			b.editInteractionResponse(s, i, fmt.Sprintf("Cannot access timeline for **%s**. A confirmation message has been sent below.", username))

			confirmMsgContent := fmt.Sprintf("%s, do you want to add **%s** for **live notifications only**? React with ✅ to confirm or ❌ to cancel.", i.Member.Mention(), username)
			msg, err := s.ChannelMessageSend(i.ChannelID, confirmMsgContent)
			if err != nil {
				log.Printf("Error sending confirmation message: %v", err)
				return
			}
			s.MessageReactionAdd(i.ChannelID, msg.ID, "✅")
			s.MessageReactionAdd(i.ChannelID, msg.ID, "❌")

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			reactionChan := make(chan string)
			handlerID := s.AddHandler(func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
				if r.MessageID != msg.ID || r.UserID == s.State.User.ID || r.UserID != i.Member.User.ID {
					return
				}
				if r.Emoji.Name == "✅" || r.Emoji.Name == "❌" {
					select {
					case reactionChan <- r.Emoji.Name:
					default:
					}
				}
			})
			defer handlerID()

			select {
			case reaction := <-reactionChan:
				if reaction == "✅" {
					user := &models.MonitoredUser{
						GuildID: i.GuildID, UserID: accountInfo.ID, Username: username, NotificationChannel: channel.ID, PostNotificationChannel: channel.ID,
						LiveNotificationChannel: channel.ID, LastPostID: "", LastStreamStart: 0, MentionRole: mentionRole, AvatarLocation: avatarLocation,
						AvatarLocationUpdatedAt: time.Now().Unix(), LiveImageURL: "", PostsEnabled: false, LiveEnabled: true, LiveMentionRole: mentionRole, PostMentionRole: mentionRole,
					}
					if err := database.NewRepository().AddOrUpdateMonitoredUser(user); err != nil {
						s.ChannelMessageEdit(i.ChannelID, msg.ID, fmt.Sprintf("Error adding user: %v", err))
					} else {
						s.ChannelMessageEdit(i.ChannelID, msg.ID, fmt.Sprintf("✅ Added **%s** for live notifications only.", username))
					}
				} else {
					s.ChannelMessageEdit(i.ChannelID, msg.ID, "❌ Operation cancelled.")
				}
			case <-ctx.Done():
				s.ChannelMessageEdit(i.ChannelID, msg.ID, "Confirmation timed out.")
			}
			time.AfterFunc(10*time.Second, func() { s.ChannelMessageDelete(i.ChannelID, msg.ID) })
			return
		}

		repo := database.NewRepository()
		user := &models.MonitoredUser{
			GuildID: i.GuildID, UserID: accountInfo.ID, Username: username, NotificationChannel: channel.ID, PostNotificationChannel: channel.ID,
			LiveNotificationChannel: channel.ID, LastPostID: "", LastStreamStart: 0, MentionRole: mentionRole, AvatarLocation: avatarLocation,
			AvatarLocationUpdatedAt: time.Now().Unix(), LiveImageURL: "", PostsEnabled: true, LiveEnabled: true, LiveMentionRole: mentionRole, PostMentionRole: mentionRole,
		}

		err = repo.AddOrUpdateMonitoredUser(user)
		if err != nil {
			b.editInteractionResponse(s, i, fmt.Sprintf("Error storing user in database: %v", err))
			return
		}

		b.editInteractionResponse(s, i, fmt.Sprintf("Successfully added **%s** to the monitoring list for all notifications.", username))
	}()
}

func (b *Bot) handleSetColorCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	options := i.ApplicationCommandData().Options
	username := options[0].StringValue()
	notifType := options[1].StringValue()
	colorHex := options[2].StringValue()

	if !hexColorRegex.MatchString(colorHex) {
		b.editInteractionResponse(s, i, "Invalid hex color format. Please use `#[6-digit code]`, for example: `#5865F2`.")
		return
	}

	colorInt, err := strconv.ParseInt(strings.TrimPrefix(colorHex, "#"), 16, 32)
	if err != nil {
		b.editInteractionResponse(s, i, "Could not parse the provided color. Please check the format.")
		return
	}

	repo := database.NewRepository()
	monitoredUser, err := repo.GetMonitoredUserByUsername(i.GuildID, username)
	if err != nil {
		log.Printf("Error fetching monitored user by username '%s': %v", username, err)
		b.editInteractionResponse(s, i, "An error occurred while looking up the creator.")
		return
	}
	if monitoredUser == nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Creator **%s** is not being monitored in this server. Please add them first.", username))
		return
	}

	colors, err := repo.GetEmbedColors(i.GuildID, monitoredUser.UserID)
	if err != nil {
		log.Printf("Error fetching existing embed colors for user %s: %v", monitoredUser.UserID, err)
		b.editInteractionResponse(s, i, "An error occurred while fetching color settings.")
		return
	}
	if colors == nil {
		colors = &models.UserEmbedColor{
			GuildID: i.GuildID,
			UserID:  monitoredUser.UserID,
		}
	}

	switch notifType {
	case "posts":
		colors.PostEmbedColor = int(colorInt)
	case "live":
		colors.LiveEmbedColor = int(colorInt)
	default:
		b.editInteractionResponse(s, i, "Invalid notification type selected.")
		return
	}

	if err := repo.UpsertEmbedColors(colors); err != nil {
		log.Printf("Error upserting embed colors: %v", err)
		b.editInteractionResponse(s, i, "Failed to save the new color setting to the database.")
		return
	}

	responseMessage := fmt.Sprintf(
		"✅ Successfully set the **%s** notification color for **%s** to `%s`.",
		notifType,
		username,
		strings.ToUpper(colorHex),
	)
	b.editInteractionResponse(s, i, responseMessage)
}

func (b *Bot) handleServersCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	guilds := b.Session.State.Guilds
	if len(guilds) == 0 {
		b.editInteractionResponse(s, i, "The bot is not currently in any servers.")
		return
	}

	sort.Slice(guilds, func(i, j int) bool {
		return guilds[i].Name < guilds[j].Name
	})

	var serverDetails []string
	for _, guild := range guilds {
		line := fmt.Sprintf("**%s**\n  `ID:` %s\n  `Members:` %d", guild.Name, guild.ID, guild.MemberCount)
		serverDetails = append(serverDetails, line)
	}

	requestedPage := 1
	if len(i.ApplicationCommandData().Options) > 0 {
		requestedPage = int(i.ApplicationCommandData().Options[0].IntValue())
		requestedPage = max(1, requestedPage)
	}

	b.sendPaginatedList(s, i, serverDetails, requestedPage)
}

func (b *Bot) handleLeaveCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	identifier := i.ApplicationCommandData().Options[0].StringValue()
	var targetGuild *discordgo.Guild

	for _, guild := range b.Session.State.Guilds {
		if guild.ID == identifier || guild.Name == identifier {
			targetGuild = guild
			break
		}
	}

	if targetGuild == nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error: Could not find a server with the name or ID `%s`.", identifier))
		return
	}

	err = s.GuildLeave(targetGuild.ID)
	if err != nil {
		log.Printf("Failed to leave guild %s (%s): %v", targetGuild.Name, targetGuild.ID, err)
		b.editInteractionResponse(s, i, fmt.Sprintf("An error occurred while trying to leave **%s**.", targetGuild.Name))
		return
	}

	log.Printf("Bot was instructed to leave guild %s (%s) by the owner.", targetGuild.Name, targetGuild.ID)
	b.editInteractionResponse(s, i, fmt.Sprintf("✅ Successfully left **%s**.", targetGuild.Name))
}

func (b *Bot) handleRemoveCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	username := i.ApplicationCommandData().Options[0].StringValue()

	repo := database.NewRepository()
	err = repo.DeleteMonitoredUserByUsername(i.GuildID, username)
	if err != nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error removing user: %v", err))
		return
	}

	b.editInteractionResponse(s, i, fmt.Sprintf("Removed **%s** from the monitoring list.", username))
}

func (b *Bot) respondToInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, content string, ephemeral bool) {
	flags := discordgo.MessageFlags(0)
	if ephemeral {
		flags = discordgo.MessageFlagsEphemeral
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   flags,
		},
	})
}

func (b *Bot) handleListCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	requestedPage := 1
	if len(i.ApplicationCommandData().Options) > 0 {
		requestedPage = int(i.ApplicationCommandData().Options[0].IntValue())
		requestedPage = max(1, requestedPage)
	}

	repo := database.NewRepository()
	users, err := repo.GetMonitoredUsersForGuild(i.GuildID)
	if err != nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error fetching monitored users: %v", err))
		return
	}

	if len(users) == 0 {
		b.editInteractionResponse(s, i, "No models are currently being monitored.")
		return
	}

	var monitoredUsers []string
	for _, user := range users {
		postChannelInfo := fmt.Sprintf("<#%s>", user.PostNotificationChannel)
		liveChannelInfo := fmt.Sprintf("<#%s>", user.LiveNotificationChannel)
		roleInfoPost := getRoleName(user.PostMentionRole)
		roleInfoLive := getRoleName(user.LiveMentionRole)

		postStatus := "✅ Enabled"
		if !user.PostsEnabled {
			postStatus = "❌ Disabled"
		}
		liveStatus := "✅ Enabled"
		if !user.LiveEnabled {
			liveStatus = "❌ Disabled"
		}

		userInfo := fmt.Sprintf("- **%s**\n  • Posts: %s (in %s | Role: %s)\n  • Live: %s (in %s | Role: %s)",
			user.Username,
			postStatus, postChannelInfo, roleInfoPost,
			liveStatus, liveChannelInfo, roleInfoLive,
		)
		monitoredUsers = append(monitoredUsers, userInfo)
	}

	b.sendPaginatedList(s, i, monitoredUsers, requestedPage)
}

func (b *Bot) handleSetLiveImageCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error acknowledging interaction: %v", err)
		return
	}

	options := i.ApplicationCommandData().Options
	username := options[0].StringValue()

	var imageURL string
	if attachments := i.ApplicationCommandData().Resolved.Attachments; len(attachments) > 0 {
		for _, attachment := range attachments {
			imageURL = attachment.URL
			break
		}
	}

	if imageURL == "" {
		b.editInteractionResponse(s, i, "Please attach an image to set as the live image.")
		return
	}

	repo := database.NewRepository()
	err = repo.UpdateLiveImageURL(i.GuildID, username, imageURL)
	if err != nil {
		log.Printf("Error updating live image URL: %v", err)
		b.editInteractionResponse(s, i, fmt.Sprintf("An error occurred while setting the live image: %v", err))
		return
	}

	b.editInteractionResponse(s, i, fmt.Sprintf("Live image for **%s** has been set successfully.", username))
}

func (b *Bot) handleToggleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	options := i.ApplicationCommandData().Options
	username := options[0].StringValue()
	notifiType := options[1].StringValue()
	enabled := options[2].BoolValue()

	repo := database.NewRepository()
	var updateErr error

	switch notifiType {
	case "posts":
		if enabled {
			updateErr = repo.EnablePostsByUsername(i.GuildID, username)
		} else {
			updateErr = repo.DisablePostsByUsername(i.GuildID, username)
		}
	case "live":
		if enabled {
			updateErr = repo.EnableLiveByUsername(i.GuildID, username)
		} else {
			updateErr = repo.DisableLiveByUsername(i.GuildID, username)
		}
	default:
		b.editInteractionResponse(s, i, "Invalid notification type selected.")
		return
	}

	if updateErr != nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error updating settings: %v", updateErr))
		return
	}

	status := "enabled"
	if !enabled {
		status = "disabled"
	}

	b.editInteractionResponse(s, i, fmt.Sprintf("`%s` notifications have been **%s** for **%s**.", notifiType, status, username))
}

func (b *Bot) handleSetChannelCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	options := i.ApplicationCommandData().Options
	username := options[0].StringValue()
	notifType := options[1].StringValue()
	channel := options[2].ChannelValue(s)

	repo := database.NewRepository()
	var updateErr error

	switch notifType {
	case "posts":
		updateErr = repo.UpdatePostChannel(i.GuildID, username, channel.ID)
	case "live":
		updateErr = repo.UpdateLiveChannel(i.GuildID, username, channel.ID)
	default:
		b.editInteractionResponse(s, i, "Invalid notification type.")
		return
	}

	if updateErr != nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error updating channel: %v", updateErr))
		return
	}

	b.editInteractionResponse(s, i, fmt.Sprintf("Successfully set the %s notification channel for **%s** to %s.", notifType, username, channel.Mention()))
}

func (b *Bot) handleSetPostMentionCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	options := i.ApplicationCommandData().Options
	username := options[0].StringValue()
	var roleID string
	var roleMention string

	if len(options) > 1 {
		role := options[1].RoleValue(s, i.GuildID)
		if role != nil {
			roleID = role.ID
			roleMention = role.Mention()
		}
	}

	repo := database.NewRepository()
	err = repo.UpdatePostMentionRole(i.GuildID, username, roleID)
	if err != nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error updating post mention role: %v", err))
		return
	}

	message := fmt.Sprintf("Post mention role for **%s** has been cleared.", username)
	if roleID != "" {
		message = fmt.Sprintf("Post mention role for **%s** set to %s.", username, roleMention)
	}
	b.editInteractionResponse(s, i, message)
}

func (b *Bot) handleSetLiveMentionCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Error deferring interaction: %v", err)
		return
	}

	options := i.ApplicationCommandData().Options
	username := options[0].StringValue()
	var roleID string
	var roleMention string

	if len(options) > 1 {
		role := options[1].RoleValue(s, i.GuildID)
		if role != nil {
			roleID = role.ID
			roleMention = role.Mention()
		}
	}

	repo := database.NewRepository()
	err = repo.UpdateLiveMentionRole(i.GuildID, username, roleID)
	if err != nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error updating live mention role: %v", err))
		return
	}

	message := fmt.Sprintf("Live mention role for **%s** has been cleared.", username)
	if roleID != "" {
		message = fmt.Sprintf("Live mention role for **%s** set to %s.", username, roleMention)
	}
	b.editInteractionResponse(s, i, message)
}

func (b *Bot) handleSetLimitCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !b.isBotOwner(i) {
		b.respondToInteraction(s, i, "This command is for the bot owner only.", true)
		return
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	options := i.ApplicationCommandData().Options
	guildID := options[0].StringValue()
	limit := options[1].IntValue()
	durationDays := int64(0)
	if len(options) > 2 {
		durationDays = options[2].IntValue()
	}

	var expiresAt int64
	if durationDays > 0 {
		expiresAt = time.Now().Add(time.Duration(durationDays) * 24 * time.Hour).Unix()
	} else {
		expiresAt = time.Now().AddDate(100, 0, 0).Unix()
	}

	sub := &models.GuildSubscription{
		GuildID:          guildID,
		SubscriptionTier: "manual-override",
		UserLimit:        int(limit),
		ExpiresAt:        expiresAt,
	}

	repo := database.NewRepository()
	if err := repo.UpsertGuildSubscription(sub); err != nil {
		b.editInteractionResponse(s, i, fmt.Sprintf("Error setting limit: %v", err))
		return
	}

	b.editInteractionResponse(s, i, fmt.Sprintf("Successfully set user limit for server `%s` to **%d**.", guildID, limit))
}

func (b *Bot) handleSetFormatCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := i.ApplicationCommandData().Options
	username := options[0].StringValue()
	notifType := options[1].StringValue()

	user, err := b.Repo.GetMonitoredUserByUsername(i.GuildID, username)
	if err != nil || user == nil {
		b.respondToInteraction(s, i, fmt.Sprintf("Creator **%s** is not being monitored in this server.", username), true)
		return
	}

	formats, _ := b.Repo.GetNotificationFormats(user.GuildID, user.UserID)
	var currentFormat, modalTitle, placeholder, label string

	if notifType == "posts" {
		modalTitle = fmt.Sprintf("Set Post Format for %s", username)
		label = "Post Notification Message"
		placeholder = "e.g., Hey {postMention}, {username} just posted!"
		if formats != nil {
			currentFormat = formats.PostMessageFormat
		}
	} else { // "live"
		modalTitle = fmt.Sprintf("Set Live Format for %s", username)
		label = "Live Notification Message"
		placeholder = "e.g., {liveMention}! {username} is now live!"
		if formats != nil {
			currentFormat = formats.LiveMessageFormat
		}
	}

	// Use the original notifType ("posts" or "live") in the CustomID
	customID := fmt.Sprintf("format_modal_%s_%s", notifType, user.UserID)

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: customID,
			Title:    modalTitle,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "message_format_input",
							Label:       label,
							Style:       discordgo.TextInputParagraph,
							Placeholder: placeholder,
							Value:       currentFormat,
							Required:    false,
						},
					},
				},
			},
		},
	})

	if err != nil {
		log.Printf("Error responding with modal: %v", err)
	}
}

func (b *Bot) handleFormatModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()

	parts := strings.Split(data.CustomID, "_")
	if len(parts) != 4 {
		log.Printf("Received malformed modal custom ID: %s", data.CustomID)
		return
	}
	notifType := parts[2] // This will now be "posts" or "live"
	userID := parts[3]

	messageFormat := data.Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value

	formats, err := b.Repo.GetNotificationFormats(i.GuildID, userID)
	if err != nil {
		b.respondToInteraction(s, i, "Error fetching existing data. Please try again.", true)
		return
	}
	if formats == nil {
		formats = &models.UserNotificationFormat{
			GuildID: i.GuildID,
			UserID:  userID,
		}
	}

	// Correctly check for "posts" (plural)
	if notifType == "posts" {
		formats.PostMessageFormat = messageFormat
	} else if notifType == "live" {
		formats.LiveMessageFormat = messageFormat
	}

	if err := b.Repo.UpsertNotificationFormats(formats); err != nil {
		log.Printf("Error saving format to DB: %v", err)
		b.respondToInteraction(s, i, "Failed to save the custom message format.", true)
		return
	}

	responseMessage := fmt.Sprintf("✅ Successfully updated the **%s** notification message format.", notifType)
	b.respondToInteraction(s, i, responseMessage, true)
}

func getRoleName(roleID string) string {
	if roleID == "" || roleID == "0" {
		return "None"
	}
	return fmt.Sprintf("<@&%s>", roleID)
}

func (b *Bot) editInteractionResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
	if err != nil {
		log.Printf("Error editing interaction response: %v", err)
	}
}

func (b *Bot) hasAdminOrModPermissions(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	if i.GuildID == "" {
		return false
	}

	if i.Member.Permissions&discordgo.PermissionAdministrator == discordgo.PermissionAdministrator {
		return true
	}

	if i.Member.Permissions&discordgo.PermissionManageGuild == discordgo.PermissionManageGuild {
		return true
	}

	guild, err := s.State.Guild(i.GuildID)
	if err == nil && guild.OwnerID == i.Member.User.ID {
		return true
	}

	return false
}

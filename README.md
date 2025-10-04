# NotiFansly Bot

> [!WARNING]
> This is an unofficial fansly bot for discord to be notified of new post and when a creator goes live on the platform. This only works if the creators profile either has no requirements for being viewed or just needing to be followed to view. No actual content is leaked via this bot if used via provided bot link or if self ran with a basic account just following.

[Add to your server](https://notifansly.xyz/)

## TODO:

- [ ] Improve notification sending time at scale

## Documentation

For complete setup guides and configuration instructions, visit our documentation:

**📖 [Full Documentation](https://notifansly.xyz/docs/self-hosted/creator-bot)**

- [Self-Hosted Bot Setup](https://notifansly.xyz/docs/self-hosted) - For basic notification forwarding



## Running The Bot Yourself 

Firstly download or clone the repository:

```bash
git clone github.com/NotiFansly/notifansly-bot && cd notifansly-bot

# Create the .env to configure
cp .env-example .env

# Running the program
go run .

# Building Binary
go build -v -ldflags "-w -s" -o notifansly ./cmd/notifansly/

# Running the binary 
./notifansly
```

## Configuring The .env File

To run this bot you will need to get BOTH your discord bots token and other items, and your fansly account token.

### Discord Bot token 

To get the needed discord values for the .env file, you can read and follow the instructions from [discords developrs doc's](https://discord.com/developers/docs/quick-start/getting-started#step-1-creating-an-app) 

### Fansly Token

1. Go to [fansly.com](https://fansly.com) and login
2. Open developer tools (Ctrl+Shift+I / F12)
3. Go to the Console tab and paste:

```javascript
console.clear();
const activeSession = localStorage.getItem("session_active_session");
const { token } = JSON.parse(activeSession);
console.log('%c➡️ Authorization_Token:', 'font-size: 12px; color: limegreen; font-weight: bold;', token);
console.log('%c➡️ User_Agent:', 'font-size: 12px; color: yellow; font-weight: bold;', navigator.userAgent);
```

4. Copy the displayed values to your `.env` file

## Support

Need help? Join our support server: [https://discord.gg/WXr8Zd2Js7](https://discord.gg/WXr8Zd2Js7)

## Disclaimer 
> [!CAUTION]
> Use at your own risk. The creator of this program is not responsible for any outcomes that may take place upon the end users' account for using this program. This program is not affiliated or endorsed by "Fansly" or Select Media LLC, the operator of "Fansly". 

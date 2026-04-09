# RedBookC-Go v0.1.0 Release Notes

**Version:** v0.1.0  
**Date:** 2026-04-09  
**Status:** Initial Release  

---

## What's New

RedBookC-Go is a小红书 (Xiaohongshu/RED) AI automated posting agent written in Go. It automatically generates content from RSS signals and publishes to Xiaohongshu using Playwright browser automation.

### Core Features

- **RSS Signal Engine** — Fetches content from WeChat Official Account RSS feeds
- **AI Content Generation** — Uses Claude API to generate Xiaohongshu-style posts (emoji, hashtags, <20 chars)
- **Dual Publishing Modes**:
  - **Auto Mode**: signal → generate → publish (fully automated)
  - **Review Mode**: signal → generate → webhook notification → user approval → publish
- **Playwright Browser Automation** — Real Chrome browser control with anti-detection
- **Multi-Account Management** — Isolated Chrome Profiles per account
- **Token System** — Users provide their own Claude API keys

---

## Technical Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.21+ |
| Web Framework | Gin |
| Database | SQLite |
| Browser Automation | Playwright |
| AI | Claude API (haiku-4-5 model) |

---

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│ RSS Feeds   │────▶│ Signal Engine │────▶│ Claude Gen   │
└─────────────┘     └──────────────┘     └─────────────┘
                                                │
                    ┌───────────────────────────┤
                    ▼                           ▼
            ┌──────────────┐           ┌──────────────┐
            │ Auto Publish │           │ Webhook Notif │
            │  (Playwright)│           │ (Review Mode)│
            └──────────────┘           └──────────────┘
```

---

## Security Features

- Webhook URL validation (HTTPS + no internal IPs)
- Token authentication with HMAC-SHA256
- Rate limiting middleware
- Sensitive fields excluded from API responses
- Timing-attack safe secret comparison

---

## Database Schema

- `users` — User accounts
- `accounts` — Xiaohongshu accounts with Chrome Profile
- `signals` — RSS signal storage
- `jobs` — Publishing queue with publish_mode (auto/review)
- `daily_stats` — Posting statistics
- `webhook_logs` — Notification delivery logs

---

## Known Limitations

- Playwright browser initialization requires Chrome/Chromium installed
- Claude API key must be provided per account
- No test suite in v0.1.0 (manual testing required)
- No GitHub remote configured — PR workflow not available

---

## Installation

```bash
git clone <repo>
cd redbookc-go
go mod download
go build -o redbookc-go ./cmd/server

# Run
./redbookc-go
```

---

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_PATH` | No | SQLite DB path (default: ~/.redbookc-go/data.db) |
| `TOKEN_SECRET` | Yes | Secret for HMAC-SHA256 token validation |
| `WEBHOOK_SECRET` | Yes | Secret for webhook callback verification |
| `PORT` | No | HTTP server port (default: 8080) |

---

## Development

Built with Gstack development methodology:

1. `/office-hours` — Product design
2. `/plan-ceo-review` — Strategy validation
3. `/plan-eng-review` — Architecture review
4. `/review` — Code review (23 issues found, 7 critical fixed)
5. `/qa` — QA testing (5 blocking issues found and fixed)

---

## Next Steps (v0.2.0)

- [ ] Playwright E2E test suite
- [ ] Claude prompt A/B testing
- [ ] Weibo/WeChat trending signal sources
- [ ] Docker compose deployment
- [ ] Multi-user dashboard
- [ ] Payment integration (monthly subscription)

---

*Built with Gstack — AI-powered development workflow*

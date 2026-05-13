package bot

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"

	"github.com/dzarlax/readeck_toolkit/internal/readeck"
)

var (
	urlRe     = regexp.MustCompile(`https?://[^\s]+`)
	hashtagRe = regexp.MustCompile(`#([\p{L}\p{N}_-]+)`)
)

// Bot wires telebot.v3 to the Readeck client, with per-tenant token lookup.
type Bot struct {
	cfg *Config
	bot *tele.Bot
	log *slog.Logger
}

// New returns a Bot ready to Start.
func New(cfg *Config, log *slog.Logger) (*Bot, error) {
	pref := tele.Settings{
		Token:  cfg.Telegram.Token,
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}
	tb, err := tele.NewBot(pref)
	if err != nil {
		return nil, err
	}
	b := &Bot{cfg: cfg, bot: tb, log: log}
	b.register()
	return b, nil
}

func (b *Bot) register() {
	b.bot.Handle("/start", b.handleStart)
	b.bot.Handle("/whoami", b.handleWhoAmI)
	b.bot.Handle(tele.OnText, b.handleText)
}

// Start blocks on Telegram long-polling.
func (b *Bot) Start() {
	b.log.Info("bot started", "username", b.bot.Me.Username)
	b.bot.Start()
}

func (b *Bot) handleStart(c tele.Context) error {
	if _, ok := b.cfg.TokenFor(c.Sender().ID); !ok {
		return c.Reply("Not authorised. Ask the admin to add your Telegram ID to config.toml.")
	}
	return c.Reply("Send me a URL and I'll save it to Readeck. Append #tags to set labels.")
}

func (b *Bot) handleWhoAmI(c tele.Context) error {
	return c.Reply(fmt.Sprintf("Your Telegram ID: %d", c.Sender().ID))
}

func (b *Bot) handleText(c tele.Context) error {
	token, ok := b.cfg.TokenFor(c.Sender().ID)
	if !ok {
		b.log.Warn("rejected non-tenant message", "tg_id", c.Sender().ID, "username", c.Sender().Username)
		return nil // silent — don't tip off scanners that the bot exists
	}
	urls := urlRe.FindAllString(c.Text(), -1)
	if len(urls) == 0 {
		return c.Reply("Send a URL (with optional #tags).")
	}
	labels := extractHashtags(c.Text())
	client := readeck.NewClient(b.cfg.Readeck.BaseURL, token)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var msgs []string
	for _, u := range urls {
		bm, err := client.CreateBookmark(ctx, readeck.CreateInput{URL: u, Labels: labels})
		if err != nil {
			b.log.Error("create bookmark", "err", err, "url", u, "tg_id", c.Sender().ID)
			msgs = append(msgs, fmt.Sprintf("❌ %s\n%s", u, shortErr(err)))
			continue
		}
		link := readeck.PermalinkOf(b.cfg.Readeck.BaseURL, bm.ID)
		title := bm.Title
		if title == "" {
			title = u
		}
		msgs = append(msgs, fmt.Sprintf("✅ %s\n%s", title, link))
	}
	return c.Reply(strings.Join(msgs, "\n\n"))
}

func extractHashtags(text string) []string {
	m := hashtagRe.FindAllStringSubmatch(text, -1)
	if len(m) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var labels []string
	for _, x := range m {
		if _, ok := seen[x[1]]; ok {
			continue
		}
		seen[x[1]] = struct{}{}
		labels = append(labels, x[1])
	}
	return labels
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

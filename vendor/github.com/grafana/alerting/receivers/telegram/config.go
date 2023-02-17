package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/alerting/receivers"
	"github.com/grafana/alerting/templates"
)

const DefaultTelegramParseMode = "HTML"

// SupportedParseMode is a map of all supported values for field `parse_mode`. https://core.telegram.org/bots/api#formatting-options.
// Keys are options accepted by Grafana API, values are options accepted by Telegram API
var SupportedParseMode = map[string]string{"Markdown": "Markdown", "MarkdownV2": "MarkdownV2", DefaultTelegramParseMode: "HTML", "None": ""}

type Config struct {
	BotToken             string `json:"bottoken,omitempty" yaml:"bottoken,omitempty"`
	ChatID               string `json:"chatid,omitempty" yaml:"chatid,omitempty"`
	Message              string `json:"message,omitempty" yaml:"message,omitempty"`
	ParseMode            string `json:"parse_mode,omitempty" yaml:"parse_mode,omitempty"`
	DisableNotifications bool   `json:"disable_notifications,omitempty" yaml:"disable_notifications,omitempty"`
}

func ValidateConfig(jsonData json.RawMessage, decryptFn receivers.DecryptFunc) (Config, error) {
	settings := Config{}
	err := json.Unmarshal(jsonData, &settings)
	if err != nil {
		return settings, fmt.Errorf("failed to unmarshal settings: %w", err)
	}
	settings.BotToken = decryptFn("bottoken", settings.BotToken)
	if settings.BotToken == "" {
		return settings, errors.New("could not find Bot Token in settings")
	}
	if settings.ChatID == "" {
		return settings, errors.New("could not find Chat Id in settings")
	}
	if settings.Message == "" {
		settings.Message = templates.DefaultMessageEmbed
	}
	// if field is missing, then we fall back to the previous default: HTML
	if settings.ParseMode == "" {
		settings.ParseMode = DefaultTelegramParseMode
	}
	found := false
	for parseMode, value := range SupportedParseMode {
		if strings.EqualFold(settings.ParseMode, parseMode) {
			settings.ParseMode = value
			found = true
			break
		}
	}
	if !found {
		return settings, fmt.Errorf("unknown parse_mode, must be Markdown, MarkdownV2, HTML or None")
	}
	return settings, nil
}
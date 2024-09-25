package mensa

import (
	"fmt"
	"strings"

	utils "pbaobot/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Stores all information about a menu item
type MenuItem struct {
	Location    string // which mensa
	Category    string
	Title       string
	Description string
	ImageURL    string
	Price       string
	Type        string // lunch or dinner
}

// Send all mensa menus given the meal type, one menu per message with image
func SendMensaMenues(bot *tgbotapi.BotAPI, message *tgbotapi.Message, mealType string, logger *utils.BotLogger) {
	menus, err := AllEthMenus()
	if err != nil {
		logger.Errorf("Error fetching menus: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Sorry, I couldn't fetch the menus. Please try again later.")
		bot.Send(msg)
		return
	}

	var filteredMenus []MenuItem
	if mealType != "" {
		for _, menu := range menus {
			if menu.Type == mealType {
				filteredMenus = append(filteredMenus, menu)
			}
		}
	} else {
		filteredMenus = menus
	}
	menus = filteredMenus

	for _, menu := range menus {
		var text strings.Builder
		text.WriteString(fmt.Sprintf("*%s - %s*\n", menu.Location, menu.Type))
		text.WriteString(fmt.Sprintf("Category: %s\n", menu.Category))
		text.WriteString(fmt.Sprintf("Title: %s\n", menu.Title))
		text.WriteString(fmt.Sprintf("Description: %s\n", menu.Description))
		text.WriteString(fmt.Sprintf("Price: %s\n", menu.Price))

		msg := tgbotapi.NewMessage(message.Chat.ID, text.String())
		msg.ParseMode = "Markdown"

		if menu.ImageURL != "" {
			photo := tgbotapi.NewPhoto(message.Chat.ID, tgbotapi.FileURL(menu.ImageURL))
			photo.Caption = text.String()
			photo.ParseMode = "Markdown"
			_, err := bot.Send(photo)
			if err != nil {
				// Fall back to sending text message if photo fails
				_, err = bot.Send(msg)
				if err != nil {
					logger.Errorf("Error sending message: %v", err)
				}
			}
		} else {
			_, err := bot.Send(msg)
			if err != nil {
				logger.Errorf("Error sending message: %v", err)
			}
		}
	}
}

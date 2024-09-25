package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"pbaobot/mensa"
	"pbaobot/sticker"
	utils "pbaobot/utils"
	"strings"

	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

var (
	// the bot
	bot *tgbotapi.BotAPI
	// error
	err error
	// Logger
	Logger  *utils.BotLogger
	logFile *os.File
	// whether to use webhook
	useWebhook bool
)

// help message
const helpMessage = `Usage:
1. Send me '/mensa lunch' or '/mensa dinner' to get today's menus.
2. Send me a sticker to tag.
3. Use my inline mode to search for stickers given a tag.
4. Send me /help to show this message again.`

// init function runs automatically before the main function
// not work in render
func init() {
	// load .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file, using environment variables")
	}
}

func main() {
	initLogger()
	defer logFile.Close()

	// create the bot
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	bot, err = tgbotapi.NewBotAPI(token)
	bot.Debug = true
	if err != nil {
		Logger.Fatal(err)
	}

	tgbotapi.SetLogger(Logger)

	// switch between long polling and webhook
	useWebhook = os.Getenv("USE_WEBHOOK") == "true"
	if useWebhook {
		startWebhook()
	} else {
		startPolling()
	}

	// keep the main process running
	StartHTTPServer()
}

// Initialize the Logger
func initLogger() {
	logFile, err := os.OpenFile(os.Getenv("BOT_LOG_PATH"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening log file: ", err)
		os.Exit(1)
	}

	multiLogger := io.MultiWriter(os.Stdout, logFile)
	Logger = utils.NewBotLogger(multiLogger)
}

// Start a HTTP server for render port scanning
func StartHTTPServer() {
	gin.SetMode("release")
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Bot server is running!",
		})
	})
	// for render health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Bot server is healthy!",
		})
	})
	// for render keep alive
	router.HEAD("/keep-alive", func(c *gin.Context) {
		c.Status(200)
	})

	// listen for webhooks
	if useWebhook {
		router.POST("/"+bot.Token, func(c *gin.Context) {
			update := &tgbotapi.Update{}
			if err := c.BindJSON(&update); err != nil {
				Logger.Printf("Error binding update: %v", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": "Error binding update"})
				return
			}
			handleUpdate(*update)
			c.Status(http.StatusOK)
		})
	}

	if err := router.Run("0.0.0.0:" + os.Getenv("PORT")); err != nil {
		log.Panicf("error: %s", err)
	}
}

// Handle each update
func handleUpdate(update tgbotapi.Update) {
	var userID int64

	// Check the user authorization
	switch {
	case update.InlineQuery != nil:
		userID = update.InlineQuery.From.ID
	case update.Message != nil:
		userID = update.Message.From.ID
	default:
		return // Ignore other types of updates
	}

	if !utils.IsAuthorizedUser(userID) {
		// Optionally, send a message to unauthorized users
		if update.Message != nil {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "You are not authorized to use this bot.")
			bot.Send(msg)
		}
		return
	}

	// Handle the update
	switch {
	// Handle inline query
	case update.InlineQuery != nil:
		sticker.SearchStickers(bot, update.InlineQuery, Logger)
		break
	// Handle messages
	case update.Message != nil:
		if strings.EqualFold(update.Message.Text, "/mensa lunch") {
			mensa.SendMensaMenues(bot, update.Message, "Lunch", Logger)
		} else if strings.EqualFold(update.Message.Text, "/mensa dinner") {
			mensa.SendMensaMenues(bot, update.Message, "Dinner", Logger)
		} else if strings.HasPrefix(update.Message.Text, "/delete") {
			sticker.DeleteTag(bot, update.Message, Logger)
		} else if strings.HasPrefix(update.Message.Text, "/help") {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, helpMessage)
			bot.Send(msg)
		} else {
			sticker.TagSticker(bot, update.Message, Logger)
		}
		break
	}
}

// Use webhook to receive updates
// This is used for production
func startWebhook() {
	// Configure the webhook
	webhook, err := tgbotapi.NewWebhook(os.Getenv("WEBHOOK_URL") + bot.Token)
	webhook.AllowedUpdates = []string{"message", "inline_query"}
	if err != nil {
		Logger.Fatal(err)
	}

	_, err = bot.Request(webhook)
	if err != nil {
		Logger.Fatal(err)
	}

	info, err := bot.GetWebhookInfo()
	if err != nil {
		Logger.Fatal(err)
	}

	if info.LastErrorDate != 0 {
		Logger.Printf("Webhook last error: %s", info.LastErrorMessage)
	}

	// Start HTTP server
	StartHTTPServer()
}

// Use polling to receive updates
// This is used for local development
func startPolling() {
	// Remove any existing webhook
	_, err := bot.Request(tgbotapi.DeleteWebhookConfig{})
	if err != nil {
		Logger.Printf("Error removing webhook: %v", err)
	}

	// The maximum time for a connection to be open
	// The timer is reset every time the bot receives an update
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	u.AllowedUpdates = []string{"message", "inline_query"}

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		handleUpdate(update)
	}
}

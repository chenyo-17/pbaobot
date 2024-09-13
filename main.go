package main

import (
	utils "fbaobot/utils"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"context"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/db"
	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"golang.org/x/text/unicode/norm"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var (
	// // the badger db to store sticker tags
	// db *badger.DB
	firebaseApp *firebase.App
	firebaseDB  *db.Client
	// the bot
	bot *tgbotapi.BotAPI
	// maps user IDs to their current state: `initialState` or `tagState`
	userStates map[int64]string
	// maps user IDs to the file ID of the sticker they are currently tagging
	userCurrentSticker map[int64]string
	// states
	initialState = ""                // initial state
	tagState     = "waiting_for_tag" // waiting for the user to tag a sticker
	// error
	err error
	// authorized users
	authorizedUsersList []int64
	// Logger
	Logger  *utils.BotLogger
	logFile *os.File
	// whether to use webhook
	useWebhook bool
)

// help message
const helpMessage = "Send me a sticker to tag it, or use /delete to remove a tag"

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

	// initialize firebase db
	ctx := context.Background()
	opt := option.WithCredentialsFile(os.Getenv("FIREBASE_CREDENTIALS"))
	config := &firebase.Config{
		DatabaseURL: os.Getenv("FIREBASE_DB_URL"),
	}
	firebaseApp, err = firebase.NewApp(ctx, config, opt)
	if err != nil {
		Logger.Fatalf("Error initializing firebase app: %v", err)
	}

	firebaseDB, err = firebaseApp.Database(ctx)
	if err != nil {
		Logger.Fatalf("Error initializing firebase database: %v", err)
	}

	// parse authorized users
	authorizedUsersStrings := strings.Split(os.Getenv("AUTHORIZED_USERS"), ",")
	for _, userIDStr := range authorizedUsersStrings {
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			Logger.Printf("Error parsing user ID %s: %v", userIDStr, err)
			continue
		}
		authorizedUsersList = append(authorizedUsersList, userID)
	}

	// create the bot
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	bot, err = tgbotapi.NewBotAPI(token)
	bot.Debug = true
	if err != nil {
		Logger.Fatal(err)
	}

	tgbotapi.SetLogger(Logger)

	// initialize state maps
	userStates = make(map[int64]string)
	userCurrentSticker = make(map[int64]string)

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

// Whether a user is authorized to use the bot
func isAuthorized(userID int64) bool {
	for _, authorizedUserID := range authorizedUsersList {
		if authorizedUserID == userID {
			return true
		}
	}
	return false
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

	if !isAuthorized(userID) {
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
		searchStickers(update.InlineQuery)
		break
	// Handle messages
	case update.Message != nil:
		if strings.HasPrefix(update.Message.Text, "/delete") {
			deleteTag(update.Message)
		} else if strings.HasPrefix(update.Message.Text, "/help") {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, helpMessage)
			bot.Send(msg)
		} else {
			tagSticker(update.Message)
		}
		break
	}
}

// Delete a tag
func deleteTag(message *tgbotapi.Message) {
	parts := strings.SplitN(message.Text, " ", 2)
	if len(parts) != 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Delete a tag with /delete <tag>")
		bot.Send(msg)
		return
	}

	tagToDelete := parts[1]
	ctx := context.Background()
	ref := firebaseDB.NewRef("tags/" + tagToDelete)
	err = ref.Delete(ctx)

	if err != nil {
		Logger.Errorf("Failed to delete key: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Sorry but it failed to delete the tag. Please try again.")
		bot.Send(msg)
	} else {
		Logger.Infof("Deleted key: %s", tagToDelete)
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Successfully deleted tag: %s", tagToDelete))
		bot.Send(msg)
	}
	return
}

// State machine to tag stickers
func tagSticker(message *tgbotapi.Message) {
	userID := message.From.ID

	// initial state (state 0)
	if userStates[userID] == initialState {
		if message.Sticker != nil {
			userStates[userID] = tagState
			handleSticker(message)
			return
		} else {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Send me a sticker to tag or a tag to search.")
			bot.Send(msg)
			return
		}
	}

	// tagState (state 1)
	if userStates[userID] == tagState {
		if strings.HasPrefix(message.Text, "/abort") {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Current operation aborted.")
			bot.Send(msg)
			userStates[userID] = initialState
			delete(userCurrentSticker, userID)
			return
		} else {
			addTagToSticker(message)
			userStates[userID] = initialState
			return
		}
	}
}

// Switch state to receive a tag for a sticker
func handleSticker(message *tgbotapi.Message) {
	fileID := message.Sticker.FileID
	userID := message.From.ID
	// TODO: support multiple tags for a sticker
	msg := tgbotapi.NewMessage(message.Chat.ID, "Send me a tag for this sticker or use /abort to cancel.")
	bot.Send(msg)

	userStates[userID] = tagState
	userCurrentSticker[userID] = fileID
}

// Store a tag for a sticker
func addTagToSticker(message *tgbotapi.Message) {
	userID := message.From.ID
	tag := message.Text
	tag = norm.NFC.String(tag) // normalize unicode characters

	ctx := context.Background()
	ref := firebaseDB.NewRef("tags/" + tag)

	var stickers []string
	if err := ref.Get(ctx, &stickers); err != nil {
		Logger.Println("Error getting stickers:", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Sorry, but it failed to add the tag. Please try again.")
		bot.Send(msg)
		return
	}

	fileID := userCurrentSticker[userID]
	// the fileID changes for the same sticker,
	// no way to check duplicates
	// for _, s := range stickers {
	// 	if s == fileID {
	// 		return // Sticker already exists, skip adding
	// 	}
	// }
	stickers = append(stickers, fileID)

	if err := ref.Set(ctx, stickers); err != nil {
		Logger.Println("Error adding tag:", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Sorry, but it failed to add the tag. Please try again.")
		bot.Send(msg)
	} else {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Tag "+tag+" added.")
		bot.Send(msg)
	}

	// reset state
	delete(userStates, userID)
	delete(userCurrentSticker, userID)
}

// Search for stickers with a tag
func searchStickers(query *tgbotapi.InlineQuery) {
	// check the user authorization
	if !isAuthorized(query.From.ID) {
		return
	}

	tag := query.Query
	results := make([]interface{}, 0)

	ctx := context.Background()
	ref := firebaseDB.NewRef("tags/" + tag)

	var stickerIDs []string
	if err := ref.Get(ctx, &stickerIDs); err != nil {
		if err != iterator.Done {
			Logger.Println("Error searching stickers:", err)
		}
		// If tag not found or any other error, return empty results
		return
	}

	for id, fileID := range stickerIDs {
		result := tgbotapi.NewInlineQueryResultCachedSticker(fmt.Sprintf("%d", id+1), fileID, "")
		results = append(results, result)
	}

	if len(results) == 0 {
		// no results found
		return
	}

	inlineConf := tgbotapi.InlineConfig{
		InlineQueryID: query.ID,
		Results:       results,
	}

	if _, err := bot.Request(inlineConf); err != nil {
		Logger.Println("Error answering inline query:", err)
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

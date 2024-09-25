package sticker

import (
	"context"
	"fmt"
	"os"
	utils "pbaobot/utils"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/db"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"golang.org/x/text/unicode/norm"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

var (
	firebaseApp *firebase.App
	firebaseDB  *db.Client
	// maps user IDs to their current state: `initialState` or `tagState`
	userStates map[int64]string
	// maps user IDs to the file ID of the sticker they are currently tagging
	userCurrentSticker map[int64]string
	// authorized users
	authorizedUsersList []int64
)

const INITIAL_STATE = ""            // initial state
const TAG_STATE = "waiting_for_tag" // waiting for the user to tag a sticker

func init() {
	// load .env
	err := godotenv.Load(".env")
	if err != nil {
		fmt.Println("Error loading .env file, using environment variables")
	}

	// initialize firebase db
	ctx := context.Background()
	opt := option.WithCredentialsFile(os.Getenv("FIREBASE_CREDENTIALS"))
	config := &firebase.Config{
		DatabaseURL: os.Getenv("FIREBASE_DB_URL"),
	}
	firebaseApp, err = firebase.NewApp(ctx, config, opt)
	if err != nil {
		fmt.Printf("Error initializing firebase app: %v", err)
		os.Exit(1)
	}

	firebaseDB, err = firebaseApp.Database(ctx)
	if err != nil {
		fmt.Printf("Error initializing firebase database: %v", err)
		os.Exit(1)
	}

	// initialize state maps
	userStates = make(map[int64]string)
	userCurrentSticker = make(map[int64]string)
}

// Delete a tag
func DeleteTag(bot *tgbotapi.BotAPI, message *tgbotapi.Message, logger *utils.BotLogger) {
	parts := strings.SplitN(message.Text, " ", 2)
	if len(parts) != 2 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Delete a tag with /delete <tag>")
		bot.Send(msg)
		return
	}

	tagToDelete := parts[1]
	ctx := context.Background()
	ref := firebaseDB.NewRef("tags/" + tagToDelete)
	err := ref.Delete(ctx)

	if err != nil {
		logger.Errorf("Failed to delete key: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Sorry but it failed to delete the tag. Please try again.")
		bot.Send(msg)
	} else {
		logger.Infof("Deleted key: %s", tagToDelete)
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Successfully deleted tag: %s", tagToDelete))
		bot.Send(msg)
	}
	return
}

// State machine to tag stickers
func TagSticker(bot *tgbotapi.BotAPI, message *tgbotapi.Message, logger *utils.BotLogger) {
	userID := message.From.ID

	// initial state (state 0)
	if userStates[userID] == INITIAL_STATE {
		if message.Sticker != nil {
			userStates[userID] = TAG_STATE
			handleSticker(bot, message, logger)
			return
		} else {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Send me a sticker to tag")
			bot.Send(msg)
			return
		}
	}

	// tagState (state 1)
	if userStates[userID] == TAG_STATE {
		if strings.HasPrefix(message.Text, "/abort") {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Current operation aborted.")
			bot.Send(msg)
			userStates[userID] = INITIAL_STATE
			delete(userCurrentSticker, userID)
			return
		} else {
			addTagToSticker(bot, message, logger)
			userStates[userID] = INITIAL_STATE
			return
		}
	}
}

// Switch state to receive a tag for a sticker
func handleSticker(bot *tgbotapi.BotAPI, message *tgbotapi.Message, logger *utils.BotLogger) {
	fileID := message.Sticker.FileID
	userID := message.From.ID
	// TODO: support multiple tags for a sticker
	msg := tgbotapi.NewMessage(message.Chat.ID, "Send me a tag for this sticker or use /abort to cancel.")
	bot.Send(msg)

	userStates[userID] = TAG_STATE
	userCurrentSticker[userID] = fileID
}

// Store a tag for a sticker
func addTagToSticker(bot *tgbotapi.BotAPI, message *tgbotapi.Message, logger *utils.BotLogger) {
	userID := message.From.ID
	tag := message.Text
	tag = norm.NFC.String(tag) // normalize unicode characters

	ctx := context.Background()
	ref := firebaseDB.NewRef("tags/" + tag)

	var stickers []string
	if err := ref.Get(ctx, &stickers); err != nil {
		logger.Println("Error getting stickers:", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Sorry, but it failed to add the tag. Please try again.")
		bot.Send(msg)
		return
	}

	fileID := userCurrentSticker[userID]
	stickers = append(stickers, fileID)

	if err := ref.Set(ctx, stickers); err != nil {
		logger.Println("Error adding tag:", err)
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
func SearchStickers(bot *tgbotapi.BotAPI, query *tgbotapi.InlineQuery, logger *utils.BotLogger) {
	// check the user authorization
	if !utils.IsAuthorizedUser(query.From.ID) {
		return
	}

	tag := query.Query
	results := make([]interface{}, 0)

	ctx := context.Background()
	ref := firebaseDB.NewRef("tags/" + tag)

	var stickerIDs []string
	if err := ref.Get(ctx, &stickerIDs); err != nil {
		if err != iterator.Done {
			logger.Println("Error searching stickers:", err)
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
		logger.Println("Error answering inline query:", err)
	}
}

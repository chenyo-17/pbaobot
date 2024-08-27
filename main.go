package main

import (
	"bufio"
	"context"
	utils "fbaobot/utils"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dgraph-io/badger/v4"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	"golang.org/x/text/unicode/norm"
)

var (
	// the badger db to store sticker tags
	db *badger.DB
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
)

// help message
const helpMessage = "Send me a sticker to tag it, or use /delete to remove a tag"

// init function runs automatically before the main function
// not used in render
func init() {
	// load .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file, using environment variables")
	}

}

// Initialize the Logger.er
func initLogger() {
	logFile, err := os.OpenFile(os.Getenv("BOT_LOG_PATH"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening log file: ", err)
		os.Exit(1)
	}

	Logger = utils.NewBotLogger(logFile)
}

func main() {
	initLogger()
	defer logFile.Close()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	// check whether the environment variable is set
	if token == "" {
		Logger.Fatal("No token provided")
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
	bot, err = tgbotapi.NewBotAPI(token)
	bot.Debug = true
	if err != nil {
		Logger.Fatal(err)
	}

	// initialize the bot
	tgbotapi.SetLogger(Logger)
	u := tgbotapi.NewUpdate(0)
	// The maximum time for a connection to be open
	// The timer is reset every time the bot receives an update
	u.Timeout = 60
	u.AllowedUpdates = []string{"message", "inline_query"}

	// Create a new cancellable background context
	// This is the single context we will use to handle all updates
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	// open badger db
	opts := badger.DefaultOptions("./badger")
	opts.Logger = Logger

	db, err = badger.Open(opts)
	if err != nil {
		Logger.Fatal(err)
	}
	// start the db debug server
	go utils.StartHTTPServer(db, Logger, os.Getenv("PORT"))
	defer db.Close()

	// initialize state maps
	userStates = make(map[int64]string)
	userCurrentSticker = make(map[int64]string)

	// `updates` is a golang channel which receives telegram updates
	// automatically handles offset management (with persistency) by keeping track of the last update ID
	updates := bot.GetUpdatesChan(u)

	// Pass cancellable context to goroutine to handle updates
	// go receiveUpdates(ctx, updates)
	go receiveUpdates(ctx, updates)

	// Tell the user the bot is online
	Logger.Println("Start listening for updates. Press enter to stop")

	// Wait for a newline symbol, then cancel handling updates
	// This is only for the bot admin to stop the bot
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	cancel()

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

// Start the infinite loop to receive updates
func receiveUpdates(ctx context.Context, updates tgbotapi.UpdatesChannel) {
	for {
		select {
		// stop looping if ctx is cancelled
		case <-ctx.Done():
			return
		// receive update from channel and then handle it
		case update := <-updates:
			handleUpdate(update)
		}
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
	err := db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(tagToDelete))
	})

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

	err := db.Update(func(txn *badger.Txn) error {
		key := []byte(tag)
		var stickers []string
		item, err := txn.Get(key)
		if err == nil {
			err = item.Value(func(val []byte) error {
				stickers = strings.Split(string(val), ",")
				return nil
			})
			if err != nil {
				return err
			}
		} else if err != badger.ErrKeyNotFound {
			return err
		}
		stickers = append(stickers, userCurrentSticker[userID])
		// store the sticker with the tag
		// key: tag, value: [sticker1, sticker2, ...]
		return txn.Set(key, []byte(strings.Join(stickers, ",")))
	})

	if err != nil {
		Logger.Println("Error adding tag:", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Sorry but it failed to add the tag. Please try again.")
		bot.Send(msg)
	} else {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Added tag "+tag)
		// listDB(db)
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

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		item, err := txn.Get([]byte(tag))
		if err == nil {
			err = item.Value(func(val []byte) error {
				stickerIDs := strings.Split(string(val), ",")
				id := 1 // determines the order in the results when there are multiple
				for _, fileID := range stickerIDs {
					result := tgbotapi.NewInlineQueryResultCachedSticker(fmt.Sprintf("%d", id), fileID, "")
					results = append(results, result)
					id += 1
				}
				return nil
			})
			if err != nil {
				return err
			}
		} else if err != badger.ErrKeyNotFound {
			return err
		}
		return nil
	})

	if err != nil {
		Logger.Println("Error searching stickers:", err)
		return
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

package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
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
)

// init function runs automatically before the main function
func init() {
	// load .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

}

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")

	// create the bot
	bot, err = tgbotapi.NewBotAPI(token)
	bot.Debug = true
	if err != nil {
		log.Fatal(err)
	}

	// open badger db
	opts := badger.DefaultOptions("./badger")
	db, err = badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	// start the db debug server
	go startDebugServer(db)
	defer db.Close()

	// initialize state maps
	userStates = make(map[int64]string)
	userCurrentSticker = make(map[int64]string)

	// initialize the bot
	u := tgbotapi.NewUpdate(0)
	// The maximum time for a connection to be open
	// The timer is reset every time the bot receives an update
	u.Timeout = 60
	u.AllowedUpdates = []string{"message", "inline_query"}

	// Create a new cancellable background context
	// This is the single context we will use to handle all updates
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	// `updates` is a golang channel which receives telegram updates
	// automatically handles offset management (with persistency) by keeping track of the last update ID
	updates := bot.GetUpdatesChan(u)

	// Pass cancellable context to goroutine to handle updates
	// go receiveUpdates(ctx, updates)
	go receiveUpdates(ctx, updates)

	// Tell the user the bot is online
	log.Println("Start listening for updates. Press enter to stop")

	// Wait for a newline symbol, then cancel handling updates
	// This is only for the bot admin to stop the bot
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	cancel()

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
	switch {

	// Handle inline query
	case update.InlineQuery != nil:
		searchStickers(update.InlineQuery)
		break
	// Handle messages
	case update.Message != nil:
		tagSticker(update.Message)
		break

	}
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
		log.Println("Error adding tag:", err)
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
		log.Println("Error searching stickers:", err)
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
		log.Println("Error answering inline query:", err)
	}

}

// Debug server to view database contents
func startDebugServer(db *badger.DB) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		err := db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			it := txn.NewIterator(opts)
			defer it.Close()

			fmt.Fprintf(w, "<h1>Database Contents</h1>")
			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				k := item.Key()
				err := item.Value(func(v []byte) error {
					fmt.Fprintf(w, "<p>Key: %s, Value: %s</p>", k, v)
					return nil
				})
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	log.Println("Debug server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

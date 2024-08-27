package utils

import (
	"fmt"
	"net/http"
	"os"

	"github.com/dgraph-io/badger/v4"
)

// Start a HTTP server for render port scanning and database debugging
func StartHTTPServer(db *badger.DB, logger *BotLogger, port string) {
	// for render port scanning
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Bot is running!"))
	})
	// health check engpoint, return 200 OK
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	// for database debugging
	http.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		// add basic auth
		user, pass, ok := r.BasicAuth()
		if !ok || user != os.Getenv("DB_USER") || pass != os.Getenv("DB_PASS") {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

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

	fmt.Println("Debug server starting on :" + port)
	logger.Println("Debug server starting on :" + port)
	// logger.Fatal(http.ListenAndServe(":"+port, nil))
	addr := fmt.Sprintf("0.0.0.0:%s", port)
	server := &http.Server{
		Addr: addr,
	}
	if err := server.ListenAndServe(); err != nil {
		logger.Fatal(err)
	}
}

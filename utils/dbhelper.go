package utils

import (
	"fmt"
	"net/http"
	"os"

	"github.com/dgraph-io/badger/v4"
)

// Debug server to view database contents
func StartDebugServer(db *badger.DB, logger *BotLogger) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

	logger.Println("Debug server starting on :8080")
	logger.Fatal(http.ListenAndServe(":8080", nil))

}

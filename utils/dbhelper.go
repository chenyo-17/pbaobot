package utils

import (
	"fmt"
	"net/http"
	"os"

	"github.com/dgraph-io/badger/v4"
)

// The server serves two purposes:
// 1. view the contents of the database
// 2. render requirements
func StartHTTPServer(db *badger.DB, logger *BotLogger, port string) {
	// health check engpoint, return 200 OK
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

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

	logger.Println("Debug server starting on :" + port)
	logger.Fatal(http.ListenAndServe(":"+port, nil))

}

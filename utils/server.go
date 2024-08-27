package utils

import (
	"fmt"
	"net"
	"net/http"

	"github.com/dgraph-io/badger/v4"
)

// Start a HTTP server for render port scanning and database debugging
func StartHTTPServer(db *badger.DB, logger *BotLogger, port string) {
	// for render port scanning
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Bot is running!"))
	})
	// health check endpoint, return 200 OK
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	addr := fmt.Sprintf("0.0.0.0:%s", port)
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Printf("HTTP server starting on %s", ln.Addr().String())
	// logger.Printf("HTTP server starting on %s", addr)
	server := &http.Server{
		Addr: addr,
	}
	if err := server.Serve(ln); err != nil {
		logger.Fatal(err)
	}
	// if err := server.ListenAndServe(); err != nil {
	// 	logger.Fatal(err)
	// }
}

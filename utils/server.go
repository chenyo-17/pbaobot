package utils

import (
	"fmt"
	"net"
	"net/http"

	"github.com/dgraph-io/badger/v4"
	gin "github.com/gin-gonic/gin"
)

// Start a HTTP server for render port scanning and database debugging
func StartHTTPServer(db *badger.DB, logger *BotLogger, port string) {
	router := gin.Default()

	// for render port scanning
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Bot is running!")
	})

	// health check endpoint, return 200 OK
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	addr := fmt.Sprintf("0.0.0.0:%s", port)
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Printf("HTTP server starting on %s", ln.Addr().String())

	server := &http.Server{
		Handler: router,
	}
	if err := server.Serve(ln); err != nil {
		logger.Fatal(err)
	}
}

package mensa

import (
	"github.com/go-rod/rod"
)

func init() {
	// Create a new browser instance
	Browser = rod.New().MustConnect()
}

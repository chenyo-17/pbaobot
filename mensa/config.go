package mensa

import (
	"sync"

	"github.com/go-rod/rod"
)

// Stores all information about a menu item
type MenuItem struct {
	Location    string // which mensa
	Category    string
	Title       string
	Description string
	ImageURL    string
	Price       string
	Type        string // lunch or dinner
}

// Make the Browser accessible across multiple goroutines
var (
	Browser      *rod.Browser
	browserMutex sync.Mutex
)

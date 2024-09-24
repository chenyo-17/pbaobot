package mensa

import "time"

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

// Lunch threshold
var LUNCH_THRESHOLD = time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 14, 0, 0, 0, time.Local)

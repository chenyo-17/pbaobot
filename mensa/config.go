package mensa

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

package mensa

import (
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
)

// Maps from ETH mensa location to its ID used in
// https://ethz.ch/en/campus/erleben/gastronomie-und-einkaufen/gastronomie/menueplaene/offerDay.html?id=x
var EthMensaId = map[string]int{
	"Clausiusbar":   3,
	"PolyMensa":     9,
	"Dozentenfoyer": 5,
}

// Return the URL for the daily offer of the specified mensa
// the date is in the format "YYYY-MM-DD"
func EthDailyOfferUrl(mensa string, date string) string {
	id, ok := EthMensaId[mensa]
	if !ok {
		return ""
	}
	return fmt.Sprintf("https://ethz.ch/en/campus/erleben/gastronomie-und-einkaufen/gastronomie/menueplaene/offerDay.html?date=%s&id=%d", date, id)
}

// where to find the menu in the HTML
const EthMenuElement = "div.basecomponent.image-component--full"

// Return the scraped menu
func scrapeEthMenu(mensa string) string {
	today := time.Now().Format("2006-01-02")
	url := EthDailyOfferUrl(mensa, today)

	browser := rod.New().MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(url)

	// Wait for the page to load completely
	page.MustWaitLoad()
	divs := page.MustElements(EthMenuElement)

	var content string
	for _, div := range divs {
		content += div.MustHTML() + "\n\n"
	}
	return content
}

// Parse the menu html and return a list of menu items
func parseEthMenus(html string) ([]MenuItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var menus []MenuItem

	doc.Find(".cp-heading").Each(func(i int, heading *goquery.Selection) {
		mealTypeText := heading.Find(".cp-heading__title").Text()
		var mealType string
		if strings.Contains(strings.ToLower(mealTypeText), "lunch") {
			mealType = "Lunch"
		} else if strings.Contains(strings.ToLower(mealTypeText), "dinner") {
			mealType = "Dinner"
		}

		heading.NextUntil(".cp-heading").Find(".cp-menu").Each(func(j int, s *goquery.Selection) {
			item := MenuItem{
				Type: mealType,
			}

			item.Category = s.Find(".cp-menu__line-small").Text()
			item.Title = s.Find(".cp-menu__title").Text()
			item.Description = s.Find(".cp-menu__description").Text()

			if img := s.Find(".cp-menu__image img"); img.Length() > 0 {
				item.ImageURL, _ = img.Attr("src")
			}

			item.Price = s.Find(".cp-menu__prices .cp-menu__paragraph").Text()

			menus = append(menus, item)
		})
	})

	return menus, nil
}

// Return all eth menus of the given meal type
func AllEthMenus() ([]MenuItem, error) {
	var allMenus []MenuItem
	for mensa := range EthMensaId {
		menuElement := scrapeEthMenu(mensa)
		menus, err := parseEthMenus(menuElement)
		for i := range menus {
			menus[i].Location = mensa
		}
		if err != nil {
			return nil, err
		}
		allMenus = append(allMenus, menus...)
	}
	return allMenus, nil
}

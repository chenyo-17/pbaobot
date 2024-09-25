package mensa

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/joho/godotenv"
)

func init() {
	// load .env
	envPath := filepath.Join("..", ".env")
	err := godotenv.Load(envPath)
	if err != nil {
		fmt.Println("Error loading .env file, using environment variables")
	}
}

// Maps from ETH mensa location to its ID used in
// https://ethz.ch/en/campus/erleben/gastronomie-und-einkaufen/gastronomie/menueplaene/offerDay.html?id=x
var EthMensaId = map[string]int{
	"Clausiusbar":   3,
	"PolyMensa":     9,
	"Archimedes":    8,
	"Dozentenfoyer": 5,
}

// Return the URL for the daily offer of the specified mensa
// the date is in the format "YYYY-MM-DD"
func EthDailyOfferUrl(mensa string, date string) string {
	id, ok := EthMensaId[mensa]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%sofferDay.html?date=%s&id=%d", EthMensaUrl, date, id)
}

const EthMensaUrl = "https://ethz.ch/en/campus/erleben/gastronomie-und-einkaufen/gastronomie/menueplaene/"

// where to find the menu in the HTML
const EthMenuElement = "div.basecomponent.image-component--full"

// Return the scraped content
func scrapeEthMensaPage(mensa string) (string, error) {
	today := time.Now().Format("2006-01-02")

	// Check if the file for today's menu is already cached
	fileName := fmt.Sprintf("%s_%s.html", today, mensa)
	if _, err := os.Stat(fileName); err == nil {
		// File exists, read its content
		content, err := os.ReadFile(fileName)
		if err != nil {
			return "", fmt.Errorf("failed to read existing HTML file: %v", err)
		}
		return string(content), nil
	}

	mensaUrl := EthDailyOfferUrl(mensa, today)

	scrapeEndpoint := fmt.Sprintf("%s?api_key=%s&url=%s&render_js=true", os.Getenv("ABSTRACT_API_URL"),
		os.Getenv("ABSTRACT_API_KEY"), url.QueryEscape(mensaUrl))

	resp, err := http.Get(scrapeEndpoint)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// clean up the scraped content
	htmlContent := cleanScrapeContent(string(body))
	fileName = fmt.Sprintf("%s_%s.html", today, mensa)
	err = os.WriteFile(fileName, []byte(htmlContent), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write HTML file: %v", err)
	}

	return htmlContent, nil
}

// parse the mensa web page and return the menu items
func parseEthMenus(htmlContent string) ([]MenuItem, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	var menus []MenuItem
	var currentType string

	doc.Find(".cp-heading, .cp-week__weekday").Each(func(i int, section *goquery.Selection) {
		if section.HasClass("cp-heading") {
			// Extract the meal type (Lunch or Dinner)
			titleText := section.Find(".cp-heading__title").Text()
			if strings.Contains(strings.ToLower(titleText), "lunch") {
				currentType = "Lunch"
			} else if strings.Contains(strings.ToLower(titleText), "dinner") {
				currentType = "Dinner"
			}
		} else if section.HasClass("cp-week__weekday") {
			section.Find(".cp-week__days .cp-menu").Each(func(j int, menuSection *goquery.Selection) {
				item := MenuItem{
					Type: currentType, // Set the meal type
				}

				item.Category = menuSection.Find(".cp-menu__line-small").Text()

				titleText := strings.TrimSpace(menuSection.Find(".cp-menu__title").Text())
				if strings.HasSuffix(titleText, "Vegan") {
					titleText = strings.TrimSpace(strings.TrimSuffix(titleText, "Vegan"))
					titleText += " (Vegan)"
				} else if strings.HasSuffix(titleText, "Vegi") {
					titleText = strings.TrimSpace(strings.TrimSuffix(titleText, "Vegi"))
					titleText += " (Vegi)"
				}
				item.Title = titleText

				item.Description = strings.TrimSpace(menuSection.Find(".cp-menu__description").Text())

				if img, exists := menuSection.Find(".cp-menu__image img").Attr("src"); exists {
					item.ImageURL = img
				}

				priceText := menuSection.Find(".cp-menu__prices .cp-menu__paragraph").First().Text()
				item.Price = strings.TrimSpace(priceText)

				menus = append(menus, item)
			})
		}
	})

	return menus, nil
}

// Return all eth menus of the given meal type
func AllEthMenus() ([]MenuItem, error) {
	var allMenus []MenuItem
	for mensa := range EthMensaId {
		menuElement, err := scrapeEthMensaPage(mensa)
		if err != nil {
			return nil, err
		}
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

// clean the scraped content
func cleanScrapeContent(rawContent string) string {
	// Delete everything before <!-- START main content -->
	startIndex := strings.Index(rawContent, "<!-- START main content -->")
	if startIndex != -1 {
		rawContent = rawContent[startIndex:]
	}

	// Remove \&#34; sequences
	re := regexp.MustCompile(`\\&#34;`)
	cleaned := re.ReplaceAllString(rawContent, "")

	// Remove all newlines (both actual newlines and \n strings)
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\\n", "")

	// Replace multiple spaces with a single space
	re = regexp.MustCompile(`\s+`)
	cleaned = re.ReplaceAllString(cleaned, " ")

	// Trim leading and trailing spaces
	cleaned = strings.TrimSpace(cleaned)

	// Replace \" with "
	cleaned = strings.ReplaceAll(cleaned, `\"`, `"`)

	return cleaned
}

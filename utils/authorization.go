package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

var authorizedUsersList []int64

func init() {
	// load .env
	err := godotenv.Load(".env")
	if err != nil {
		fmt.Println("Error loading .env file, using environment variables")
	}

	// parse authorized users
	authorizedUsersStrings := strings.Split(os.Getenv("AUTHORIZED_USERS"), ",")
	for _, userIDStr := range authorizedUsersStrings {
		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			fmt.Printf("Error parsing user ID %s: %v, authorizing all users", userIDStr, err)
			continue
		}
		authorizedUsersList = append(authorizedUsersList, userID)
	}
}

// Whether a user is authorized to use the bot
func IsAuthorizedUser(userID int64) bool {
	for _, authorizedUserID := range authorizedUsersList {
		if authorizedUserID == userID {
			return true
		}
	}
	return false
}

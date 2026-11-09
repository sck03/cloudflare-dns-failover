package main

import (
	"errors"
	"net/http"
	"strings"
)

func GetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, notificationConfig())
}

func SaveConfigHandler(w http.ResponseWriter, r *http.Request) {
	var input NotificationConfig
	if err := decodeJSON(r, &input); err != nil {
		apiError(w, 400, err)
		return
	}
	releaseManagement, ok := lockManagement(w, r)
	if !ok {
		return
	}
	defer releaseManagement()
	err := UpdateConfig(func(config *Config) error {
		config.Notification = input
		return nil
	})
	if err != nil {
		apiError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "saved"})
}

type AccountPayload struct {
	Name     string `json:"name"`
	ApiToken string `json:"api_token"`
	Email    string `json:"email"`
	ApiKey   string `json:"api_key"`
}

type AccountView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	HasAPIToken bool   `json:"has_api_token"`
	HasAPIKey   bool   `json:"has_api_key"`
}

func GetCloudflareAccounts(w http.ResponseWriter, r *http.Request) {
	config := ConfigSnapshot()
	views := make([]AccountView, 0, len(config.Accounts))
	activeIndex := 0
	for i, account := range config.Accounts {
		views = append(views, AccountView{ID: account.Name, Name: account.Name,
			Email: account.Email, HasAPIToken: account.ApiToken != "", HasAPIKey: account.ApiKey != ""})
		if account.Name == config.ActiveAccount {
			activeIndex = i
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": views, "active_index": activeIndex})
}

func CreateCloudflareAccount(w http.ResponseWriter, r *http.Request) { saveAccount(w, r, false) }
func UpdateCloudflareAccount(w http.ResponseWriter, r *http.Request) { saveAccount(w, r, true) }

func saveAccount(w http.ResponseWriter, r *http.Request, update bool) {
	var input AccountPayload
	if err := decodeJSON(r, &input); err != nil {
		apiError(w, 400, err)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || strings.ContainsAny(input.Name, "/\\\x00") {
		apiError(w, 400, errors.New("a valid account name is required"))
		return
	}
	releaseManagement, ok := lockManagement(w, r)
	if !ok {
		return
	}
	defer releaseManagement()
	id := r.PathValue("id")
	if update && id != input.Name {
		if err := unreferencedAccount(id); err != nil {
			apiError(w, 409, err)
			return
		}
	}
	err := UpdateConfig(func(config *Config) error {
		index := -1
		for i, account := range config.Accounts {
			if account.Name == input.Name && (!update || account.Name != id) {
				return errors.New("account name already exists")
			}
			if update && account.Name == id {
				index = i
			}
		}
		if update && index == -1 {
			return errors.New("account not found")
		}
		account := AccountConfig{Name: input.Name, ApiToken: input.ApiToken, Email: input.Email, ApiKey: input.ApiKey}
		if update {
			previous := config.Accounts[index]
			if account.ApiToken == "" {
				account.ApiToken = previous.ApiToken
			}
			if account.ApiKey == "" {
				account.ApiKey = previous.ApiKey
			}
			if account.Email == "" {
				account.Email = previous.Email
			}
		}
		if account.ApiToken == "" && (account.ApiKey == "" || account.Email == "") {
			return errors.New("API token or email + API key is required")
		}
		if update {
			config.Accounts[index] = account
		} else {
			config.Accounts = append(config.Accounts, account)
		}
		if config.ActiveAccount == "" || update && config.ActiveAccount == id {
			config.ActiveAccount = account.Name
		}
		return nil
	})
	if err != nil {
		apiError(w, 400, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "saved"})
}

func unreferencedAccount(name string) error {
	var count int64
	query := DB.Model(&Monitor{}).Where("account_name = ?", name)
	if active := GetActiveAccountConfig(); active != nil && active.Name == name {
		query = DB.Model(&Monitor{}).Where("account_name = ? OR account_name = ''", name)
	}
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("account is used by monitors; reassign those monitors before renaming or deleting it")
	}
	return nil
}

func DeleteCloudflareAccount(w http.ResponseWriter, r *http.Request) {
	releaseManagement, ok := lockManagement(w, r)
	if !ok {
		return
	}
	defer releaseManagement()
	id := r.PathValue("id")
	if err := unreferencedAccount(id); err != nil {
		apiError(w, 409, err)
		return
	}
	err := UpdateConfig(func(config *Config) error {
		for i, account := range config.Accounts {
			if account.Name == id {
				config.Accounts = append(config.Accounts[:i], config.Accounts[i+1:]...)
				if config.ActiveAccount == id {
					config.ActiveAccount = ""
					if len(config.Accounts) > 0 {
						config.ActiveAccount = config.Accounts[0].Name
					}
				}
				return nil
			}
		}
		return errors.New("account not found")
	})
	if err != nil {
		apiError(w, 400, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "deleted"})
}

func ActivateCloudflareAccount(w http.ResponseWriter, r *http.Request) {
	releaseManagement, ok := lockManagement(w, r)
	if !ok {
		return
	}
	defer releaseManagement()
	err := UpdateConfig(func(config *Config) error {
		if accountFromConfig(*config, r.PathValue("id")) == nil {
			return errors.New("account not found")
		}
		config.ActiveAccount = r.PathValue("id")
		return nil
	})
	if err != nil {
		apiError(w, 400, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "activated"})
}

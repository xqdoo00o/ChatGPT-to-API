package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"freechatgpt/internal/tokens"

	"github.com/xqdoo00o/OpenAIAuth/auth"
)

var accounts map[string]AccountInfo

var validAccounts []string

const interval = time.Hour * 24

type AccountInfo struct {
	Password string `json:"password"`
	Times    []int  `json:"times"`
}

type TokenExp struct {
	Exp int64 `json:"exp"`
	Iat int64 `json:"iat"`
}

func getTokenExpire(tokenstring string) (time.Time, error) {
	payLoadData := strings.Split(tokenstring, ".")[1]
	// Decode payload
	payload, err := base64.RawStdEncoding.DecodeString(payLoadData)
	if err != nil {
		return time.Time{}, err
	}
	// Unmarshal payload
	var tokenExp TokenExp
	err = json.Unmarshal(payload, &tokenExp)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(tokenExp.Exp, 0), nil
}

func AppendIfNone(slice []string, i string) []string {
	for _, ele := range slice {
		if ele == i {
			return slice
		}
	}
	return append(slice, i)
}

var TimesCounter int

func getSecret() (string, tokens.Secret) {
	if len(validAccounts) != 0 {
		account := validAccounts[0]
		secret := ACCESS_TOKENS.GetSecret(account)
		if TimesCounter == 0 {
			TimesCounter = accounts[account].Times[0]
			if secret.TeamUserID != "" && len(accounts[account].Times) == 2 {
				TimesCounter += accounts[account].Times[1]
			}
		}
		TimesCounter--
		if TimesCounter == 0 {
			validAccounts = append(validAccounts[1:], account)
		}
		if secret.TeamUserID != "" {
			if TimesCounter < accounts[account].Times[0] {
				secret.TeamUserID = ""
			}
		}
		return account, secret
	} else {
		return "", tokens.Secret{}
	}
}

// Read accounts.txt and create a list of accounts
func readAccounts() {
	accounts = map[string]AccountInfo{}
	// Read accounts.txt and create a list of accounts
	if _, err := os.Stat("accounts.txt"); err == nil {
		// Each line is a proxy, put in proxies array
		file, _ := os.Open("accounts.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			// Split by :
			line := strings.Split(scanner.Text(), ":")
			length := len(line)
			if length < 2 {
				continue
			}
			var times []int
			if length == 2 {
				times = append(times, 1)
			} else {
				timeStrs := strings.Split(line[2], "/")
				for i := 0; i < len(timeStrs); i++ {
					time, err := strconv.Atoi(timeStrs[i])
					if i == 2 || err != nil || time < 1 {
						break
					}
					times = append(times, time)
				}
				if len(times) == 0 {
					times = append(times, 1)
				}
			}
			// Create an account
			accounts[line[0]] = AccountInfo{
				Password: line[1],
				Times:    times,
			}
		}
	}
}

func newTimeFunc(email string, password string, token_list map[string]tokens.Secret, cron bool) func() {
	return func() {
		updateSingleToken(email, password, token_list, cron)
	}
}

func scheduleTokenPUID() {
	// Check if access_tokens.json exists
	if stat, err := os.Stat("access_tokens.json"); os.IsNotExist(err) {
		// Create the file
		file, err := os.Create("access_tokens.json")
		if err != nil {
			panic(err)
		}
		defer file.Close()
		updateToken()
	} else {
		file, err := os.Open("access_tokens.json")
		if err != nil {
			panic(err)
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		var token_list map[string]tokens.Secret
		err = decoder.Decode(&token_list)
		if err != nil {
			updateToken()
			return
		}
		if len(token_list) == 0 {
			updateToken()
		} else {
			ACCESS_TOKENS = tokens.NewAccessToken(token_list)
			validAccounts = []string{}
			for account, info := range accounts {
				token := token_list[account].Token
				if token == "" {
					updateSingleToken(account, info.Password, nil, true)
				} else {
					var toPUIDExpire time.Duration
					var puidTime time.Time
					var toExpire time.Duration
					if token_list[account].PUID != "" {
						re := regexp.MustCompile(`\d{10,}`)
						puidIat := re.FindString(token_list[account].PUID)
						if puidIat != "" {
							puidIatInt, _ := strconv.ParseInt(puidIat, 10, 64)
							puidTime = time.Unix(puidIatInt, 0)
							toPUIDExpire = interval - time.Since(puidTime)
							if toPUIDExpire < 0 {
								updateSingleToken(account, info.Password, nil, false)
							}
						}
					}
				tokenProcess:
					token = ACCESS_TOKENS.GetSecret(account).Token
					expireTime, err := getTokenExpire(token)
					nowTime := time.Now()
					if err != nil {
						toExpire = interval - nowTime.Sub(stat.ModTime())
					} else {
						toExpire = expireTime.Sub(nowTime)
						if toExpire > 0 {
							toExpire = toExpire % interval
						}
					}
					if toPUIDExpire > 0 {
						toPUIDExpire = interval - nowTime.Sub(puidTime)
						if toExpire-toPUIDExpire > 2e9 {
							updateSingleToken(account, info.Password, nil, false)
							toPUIDExpire = 0
							goto tokenProcess
						}
					}
					if toExpire > 0 {
						validAccounts = AppendIfNone(validAccounts, account)
						f := newTimeFunc(account, info.Password, nil, true)
						time.AfterFunc(toExpire+time.Second, f)
					} else {
						updateSingleToken(account, info.Password, nil, true)
					}
				}
			}
		}
	}
}

func updateSingleToken(email string, password string, token_list map[string]tokens.Secret, cron bool) {
	if os.Getenv("CF_PROXY") != "" {
		// exec warp-cli disconnect and connect
		exec.Command("warp-cli", "disconnect").Run()
		exec.Command("warp-cli", "connect").Run()
		time.Sleep(5 * time.Second)
	}
	println("Updating access token for " + email)
	var proxy_url string
	if len(proxies) == 0 {
		proxy_url = ""
	} else {
		proxy_url = proxies[0]
		// Push used proxy to the back of the list
		proxies = append(proxies[1:], proxies[0])
	}
	authenticator := auth.NewAuthenticator(email, password, proxy_url)
	err := authenticator.RenewWithCookies()
	if err != nil {
		authenticator.ResetCookies()
		err := authenticator.Begin()
		if err != nil {
			if token_list == nil {
				ACCESS_TOKENS.Delete(email)
				for i, v := range validAccounts {
					if v == email {
						validAccounts = append(validAccounts[:i], validAccounts[i+1:]...)
						break
					}
				}
			}
			println("Location: " + err.Location)
			println("Status code: " + strconv.Itoa(err.StatusCode))
			println("Details: " + err.Details)
			return
		}
	}
	access_token := authenticator.GetAccessToken()
	puid, _ := authenticator.GetPUID()
	teamUserID, _ := authenticator.GetTeamUserID()
	if token_list != nil {
		token_list[email] = tokens.Secret{Token: access_token, PUID: puid, TeamUserID: teamUserID}
	} else {
		ACCESS_TOKENS.Set(email, access_token, puid, teamUserID)
		ACCESS_TOKENS.Save()
	}
	validAccounts = AppendIfNone(validAccounts, email)
	println("Success!")
	err = authenticator.SaveCookies()
	if err != nil {
		println(err.Details)
	}
	if cron {
		f := newTimeFunc(email, password, token_list, cron)
		time.AfterFunc(interval+time.Second, f)
	}
}

func updateToken() {
	token_list := map[string]tokens.Secret{}
	validAccounts = []string{}
	// Loop through each account
	for account, info := range accounts {
		updateSingleToken(account, info.Password, token_list, false)
	}
	// Append access token to access_tokens.json
	ACCESS_TOKENS = tokens.NewAccessToken(token_list)
	ACCESS_TOKENS.Save()
	time.AfterFunc(interval, updateToken)
}

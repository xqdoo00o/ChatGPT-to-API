package chatgpt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"freechatgpt/internal/tokens"
	"freechatgpt/typings"
	chatgpt_types "freechatgpt/typings/chatgpt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "time/tzdata"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/sha3"

	"github.com/PuerkitoBio/goquery"
	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/gin-gonic/gin"

	chatgpt_response_converter "freechatgpt/conversion/response/chatgpt"

	official_types "freechatgpt/typings/official"
)

type connInfo struct {
	conn   *websocket.Conn
	uuid   string
	expire time.Time
	ticker *time.Ticker
	lock   bool
}

var (
	client, _ = tls_client.NewHttpClient(tls_client.NewNoopLogger(), []tls_client.HttpClientOption{
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithTimeoutSeconds(600),
		tls_client.WithClientProfile(profiles.Okhttp4Android13),
	}...)
	API_REVERSE_PROXY   = os.Getenv("API_REVERSE_PROXY")
	FILES_REVERSE_PROXY = os.Getenv("FILES_REVERSE_PROXY")
	connPool            = map[string][]*connInfo{}
	userAgent           = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	answers             = map[string]string{}
	cores               = []int{8, 12, 16, 24}
	screens             = []int{3000, 4000, 6000}
	timeLocation, _     = time.LoadLocation("Asia/Shanghai")
	timeLayout          = "Mon Jan 2 2006 15:04:05"
	cachedScripts       = []string{}
	cachedDpl           = ""
)

func newRequest(method string, url string, body io.Reader, secret *tokens.Secret, deviceId string) (*http.Request, error) {
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return &http.Request{}, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Oai-Device-Id", deviceId)
	request.Header.Set("Oai-Language", "en-US")
	if secret.Token != "" {
		request.Header.Set("Authorization", "Bearer "+secret.Token)
	}
	if secret.PUID != "" {
		request.Header.Set("Cookie", "_puid="+secret.PUID+";")
	}
	if secret.TeamUserID != "" {
		request.Header.Set("Chatgpt-Account-Id", secret.TeamUserID)
	}
	return request, nil
}

func getWSURL(secret *tokens.Secret, deviceId string, retry int) (string, error) {
	request, err := newRequest(http.MethodPost, "https://chat.openai.com/backend-api/register-websocket", nil, secret, deviceId)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		if retry > 3 {
			return "", err
		}
		time.Sleep(time.Second) // wait 1s to get ws url
		return getWSURL(secret, deviceId, retry+1)
	}
	defer response.Body.Close()
	var WSSResp chatgpt_types.ChatGPTWSSResponse
	err = json.NewDecoder(response.Body).Decode(&WSSResp)
	if err != nil {
		return "", err
	}
	return WSSResp.WssUrl, nil
}

func createWSConn(url string, connInfo *connInfo, retry int) error {
	dialer := websocket.DefaultDialer
	dialer.EnableCompression = true
	dialer.Subprotocols = []string{"json.reliable.webpubsub.azure.v1"}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		if retry > 3 {
			return err
		}
		time.Sleep(time.Second) // wait 1s to recreate ws
		return createWSConn(url, connInfo, retry+1)
	}
	connInfo.conn = conn
	connInfo.expire = time.Now().Add(time.Minute * 30)
	ticker := time.NewTicker(time.Second * 8)
	connInfo.ticker = ticker
	go func(ticker *time.Ticker) {
		defer ticker.Stop()
		for {
			<-ticker.C
			if err := connInfo.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				connInfo.conn.Close()
				connInfo.conn = nil
				break
			}
		}
	}(ticker)
	return nil
}

func findAvailConn(token string, uuid string) *connInfo {
	for _, value := range connPool[token] {
		if !value.lock {
			value.lock = true
			value.uuid = uuid
			return value
		}
	}
	newConnInfo := connInfo{uuid: uuid, lock: true}
	connPool[token] = append(connPool[token], &newConnInfo)
	return &newConnInfo
}
func findSpecConn(token string, uuid string) *connInfo {
	for _, value := range connPool[token] {
		if value.uuid == uuid {
			return value
		}
	}
	return &connInfo{}
}
func UnlockSpecConn(token string, uuid string) {
	for _, value := range connPool[token] {
		if value.uuid == uuid {
			value.lock = false
		}
	}
}
func InitWSConn(secret *tokens.Secret, deviceId string, uuid string, proxy string) error {
	token := secret.Token + secret.TeamUserID
	connInfo := findAvailConn(token, uuid)
	conn := connInfo.conn
	isExpired := connInfo.expire.IsZero() || time.Now().After(connInfo.expire)
	if conn == nil || isExpired {
		if proxy != "" {
			client.SetProxy(proxy)
		}
		if conn != nil {
			connInfo.ticker.Stop()
			conn.Close()
			connInfo.conn = nil
		}
		wssURL, err := getWSURL(secret, deviceId, 0)
		if err != nil {
			return err
		}
		createWSConn(wssURL, connInfo, 0)
		if err != nil {
			return err
		}
		return nil
	} else {
		ctx, cancelFunc := context.WithTimeout(context.Background(), time.Millisecond*100)
		go func() {
			defer cancelFunc()
			for {
				_, _, err := conn.NextReader()
				if err != nil {
					break
				}
				if ctx.Err() != nil {
					break
				}
			}
		}()
		<-ctx.Done()
		err := ctx.Err()
		if err != nil {
			switch err {
			case context.Canceled:
				connInfo.ticker.Stop()
				conn.Close()
				connInfo.conn = nil
				connInfo.lock = false
				return InitWSConn(secret, deviceId, uuid, proxy)
			case context.DeadlineExceeded:
				return nil
			default:
				return nil
			}
		}
		return nil
	}
}

func SetOAICookie(uuid string) {
	u, _ := url.Parse("https://openai.com")
	client.GetCookieJar().SetCookies(u, []*http.Cookie{{
		Name:  "oai-did",
		Value: uuid,
	}})
}

type ProofWork struct {
	Difficulty string `json:"difficulty,omitempty"`
	Required   bool   `json:"required"`
	Seed       string `json:"seed,omitempty"`
}

func getParseTime() string {
	now := time.Now()
	now = now.In(timeLocation)
	return now.Format(timeLayout) + " GMT+0800 (中国标准时间)"
}
func getDpl(proxy string) bool {
	if len(cachedScripts) != 0 {
		return true
	}
	if proxy != "" {
		client.SetProxy(proxy)
	}
	request, err := http.NewRequest(http.MethodGet, "https://chat.openai.com/", nil)
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "*/*")
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	doc, _ := goquery.NewDocumentFromReader(response.Body)
	cachedScripts = nil
	doc.Find("script[src]").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if exists {
			cachedScripts = append(cachedScripts, src)
			if cachedDpl == "" {
				idx := strings.Index(src, "dpl")
				if idx >= 0 {
					cachedDpl = src[idx:]
				}
			}
		}
	})
	return len(cachedScripts) != 0
}
func getConfig() []interface{} {
	rand.New(rand.NewSource(time.Now().UnixNano()))
	core := cores[rand.Intn(4)]
	rand.New(rand.NewSource(time.Now().UnixNano()))
	screen := screens[rand.Intn(3)]
	rand.New(rand.NewSource(time.Now().UnixNano()))
	script := cachedScripts[rand.Intn(len(cachedScripts))]
	return []interface{}{core + screen, getParseTime(), int64(4294705152), 0, userAgent, script, cachedDpl}

}
func CalcProofToken(seed string, diff string, proxy string) string {
	if answers[seed] != "" {
		return answers[seed]
	}
	if !getDpl(proxy) {
		return "gAAAAABwQ8Lk5FbGpA2NcR9dShT6gYjU7VxZ4D" + base64.StdEncoding.EncodeToString([]byte(`"`+seed+`"`))
	}
	config := getConfig()
	diffLen := len(diff) / 2
	hasher := sha3.New512()
	for i := 0; i < 1000000; i++ {
		config[3] = i
		json, _ := json.Marshal(config)
		base := base64.StdEncoding.EncodeToString(json)
		hasher.Write([]byte(seed + base))
		hash := hasher.Sum(nil)
		hasher.Reset()
		if hex.EncodeToString(hash[:diffLen]) <= diff {
			answers[seed] = "gAAAAAB" + base
			return answers[seed]
		}
	}
	return "gAAAAABwQ8Lk5FbGpA2NcR9dShT6gYjU7VxZ4D" + base64.StdEncoding.EncodeToString([]byte(`"`+seed+`"`))
}

type ChatRequire struct {
	Token  string    `json:"token"`
	Proof  ProofWork `json:"proofofwork,omitempty"`
	Arkose struct {
		Required bool   `json:"required"`
		DX       string `json:"dx,omitempty"`
	} `json:"arkose"`
}

func CheckRequire(secret *tokens.Secret, deviceId string, proxy string) *ChatRequire {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	body := bytes.NewBuffer([]byte(`{}`))
	var apiUrl string
	if secret.Token == "" {
		apiUrl = "https://chat.openai.com/backend-anon/sentinel/chat-requirements"
	} else {
		apiUrl = "https://chat.openai.com/backend-api/sentinel/chat-requirements"
	}
	request, err := newRequest(http.MethodPost, apiUrl, body, secret, deviceId)
	if err != nil {
		return nil
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	var require ChatRequire
	err = json.NewDecoder(response.Body).Decode(&require)
	if err != nil {
		return nil
	}
	return &require
}

var urlAttrMap = make(map[string]string)

type urlAttr struct {
	Url         string `json:"url"`
	Attribution string `json:"attribution"`
}

func getURLAttribution(secret *tokens.Secret, deviceId string, url string) string {
	request, err := newRequest(http.MethodPost, "https://chat.openai.com/backend-api/attributions", bytes.NewBuffer([]byte(`{"urls":["`+url+`"]}`)), secret, deviceId)
	if err != nil {
		return ""
	}
	request.Header.Set("Content-Type", "application/json")
	if err != nil {
		return ""
	}
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	var attr urlAttr
	err = json.NewDecoder(response.Body).Decode(&attr)
	if err != nil {
		return ""
	}
	return attr.Attribution
}

func POSTconversation(message chatgpt_types.ChatGPTRequest, secret *tokens.Secret, deviceId string, chat_token string, proofToken string, proxy string) (*http.Response, error) {
	if proxy != "" {
		client.SetProxy(proxy)
	}

	apiUrl := "https://chat.openai.com/backend-api/conversation"
	if API_REVERSE_PROXY != "" {
		apiUrl = API_REVERSE_PROXY
	}

	arkoseToken := message.ArkoseToken
	message.ArkoseToken = ""
	// JSONify the body and add it to the request
	body_json, err := json.Marshal(message)
	if err != nil {
		return &http.Response{}, err
	}

	request, err := newRequest(http.MethodPost, apiUrl, bytes.NewReader(body_json), secret, deviceId)
	if err != nil {
		return &http.Response{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if arkoseToken != "" {
		request.Header.Set("Openai-Sentinel-Arkose-Token", arkoseToken)
	}
	if chat_token != "" {
		request.Header.Set("Openai-Sentinel-Chat-Requirements-Token", chat_token)
	}
	if proofToken != "" {
		request.Header.Set("Openai-Sentinel-Proof-Token", proofToken)
	}
	if err != nil {
		return &http.Response{}, err
	}
	response, err := client.Do(request)
	return response, err
}

// Returns whether an error was handled
func Handle_request_error(c *gin.Context, response *http.Response) bool {
	if response.StatusCode != 200 {
		// Try read response body as JSON
		var error_response map[string]interface{}
		err := json.NewDecoder(response.Body).Decode(&error_response)
		if err != nil {
			// Read response body
			body, _ := io.ReadAll(response.Body)
			c.JSON(500, gin.H{"error": gin.H{
				"message": "Unknown error",
				"type":    "internal_server_error",
				"param":   nil,
				"code":    "500",
				"details": string(body),
			}})
			return true
		}
		c.JSON(response.StatusCode, gin.H{"error": gin.H{
			"message": error_response["detail"],
			"type":    response.Status,
			"param":   nil,
			"code":    "error",
		}})
		return true
	}
	return false
}

type ContinueInfo struct {
	ConversationID string `json:"conversation_id"`
	ParentID       string `json:"parent_id"`
}

type fileInfo struct {
	DownloadURL string `json:"download_url"`
	Status      string `json:"status"`
}

func GetImageSource(wg *sync.WaitGroup, url string, prompt string, secret *tokens.Secret, deviceId string, idx int, imgSource []string) {
	defer wg.Done()
	request, err := newRequest(http.MethodGet, url, nil, secret, deviceId)
	if err != nil {
		return
	}
	response, err := client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	var file_info fileInfo
	err = json.NewDecoder(response.Body).Decode(&file_info)
	if err != nil || file_info.Status != "success" {
		return
	}
	imgSource[idx] = "[![image](" + file_info.DownloadURL + " \"" + prompt + "\")](" + file_info.DownloadURL + ")"
}

func Handler(c *gin.Context, response *http.Response, secret *tokens.Secret, deviceId string, uuid string, translated_request chatgpt_types.ChatGPTRequest, stream bool) (string, *ContinueInfo) {
	max_tokens := false

	// Create a bufio.Reader from the response body
	reader := bufio.NewReader(response.Body)

	// Read the response byte by byte until a newline character is encountered
	if stream {
		// Response content type is text/event-stream
		c.Header("Content-Type", "text/event-stream")
	} else {
		// Response content type is application/json
		c.Header("Content-Type", "application/json")
	}
	var finish_reason string
	var previous_text typings.StringStruct
	var original_response chatgpt_types.ChatGPTResponse
	var isRole = true
	var isEnd = false
	var imgSource []string
	var isWSS = false
	var convId string
	var respId string
	var wssUrl string
	var connInfo *connInfo
	var wsSeq int
	var isWSInterrupt bool = false
	var interruptTimer *time.Timer

	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		isWSS = true
		connInfo = findSpecConn(secret.Token, uuid)
		if connInfo.conn == nil {
			c.JSON(500, gin.H{"error": "No websocket connection"})
			return "", nil
		}
		var wssResponse chatgpt_types.ChatGPTWSSResponse
		json.NewDecoder(response.Body).Decode(&wssResponse)
		wssUrl = wssResponse.WssUrl
		respId = wssResponse.ResponseId
		convId = wssResponse.ConversationId
	}
	for {
		var line string
		var err error
		if isWSS {
			var messageType int
			var message []byte
			if isWSInterrupt {
				if interruptTimer == nil {
					interruptTimer = time.NewTimer(10 * time.Second)
				}
				select {
				case <-interruptTimer.C:
					c.JSON(500, gin.H{"error": "WS interrupt & new WS timeout"})
					return "", nil
				default:
					goto reader
				}
			}
		reader:
			messageType, message, err = connInfo.conn.ReadMessage()
			if err != nil {
				connInfo.ticker.Stop()
				connInfo.conn.Close()
				connInfo.conn = nil
				err := createWSConn(wssUrl, connInfo, 0)
				if err != nil {
					c.JSON(500, gin.H{"error": err.Error()})
					return "", nil
				}
				isWSInterrupt = true
				connInfo.conn.WriteMessage(websocket.TextMessage, []byte("{\"type\":\"sequenceAck\",\"sequenceId\":"+strconv.Itoa(wsSeq)+"}"))
				continue
			}
			if messageType == websocket.TextMessage {
				var wssMsgResponse chatgpt_types.WSSMsgResponse
				json.Unmarshal(message, &wssMsgResponse)
				if wssMsgResponse.Data.ResponseId != respId {
					continue
				}
				wsSeq = wssMsgResponse.SequenceId
				if wsSeq%50 == 0 {
					connInfo.conn.WriteMessage(websocket.TextMessage, []byte("{\"type\":\"sequenceAck\",\"sequenceId\":"+strconv.Itoa(wsSeq)+"}"))
				}
				base64Body := wssMsgResponse.Data.Body
				bodyByte, err := base64.StdEncoding.DecodeString(base64Body)
				if err != nil {
					continue
				}
				if isWSInterrupt {
					isWSInterrupt = false
					interruptTimer.Stop()
				}
				line = string(bodyByte)
			}
		} else {
			line, err = reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return "", nil
			}
		}
		if len(line) < 6 {
			continue
		}
		// Remove "data: " from the beginning of the line
		line = line[6:]
		// Check if line starts with [DONE]
		if !strings.HasPrefix(line, "[DONE]") {
			// Parse the line as JSON
			original_response.Message.ID = ""
			err = json.Unmarshal([]byte(line), &original_response)
			if err != nil {
				continue
			}
			if original_response.Error != nil {
				c.JSON(500, gin.H{"error": original_response.Error})
				return "", nil
			}
			if original_response.Message.ID == "" {
				continue
			}
			if original_response.ConversationID != convId {
				if convId == "" {
					convId = original_response.ConversationID
				} else {
					continue
				}
			}
			if !(original_response.Message.Author.Role == "assistant" || (original_response.Message.Author.Role == "tool" && original_response.Message.Content.ContentType != "text")) || original_response.Message.Content.Parts == nil {
				continue
			}
			if original_response.Message.Metadata.MessageType == "" || original_response.Message.Recipient != "all" {
				continue
			}
			if original_response.Message.Metadata.MessageType != "next" && original_response.Message.Metadata.MessageType != "continue" || !strings.HasSuffix(original_response.Message.Content.ContentType, "text") {
				continue
			}
			if original_response.Message.EndTurn != nil {
				isEnd = true
			}
			if len(original_response.Message.Metadata.Citations) != 0 {
				r := []rune(original_response.Message.Content.Parts[0].(string))
				offset := 0
				for _, citation := range original_response.Message.Metadata.Citations {
					rl := len(r)
					u, _ := url.Parse(citation.Metadata.URL)
					baseURL := u.Scheme + "://" + u.Host + "/"
					attr := urlAttrMap[baseURL]
					if attr == "" {
						attr = getURLAttribution(secret, deviceId, baseURL)
						if attr != "" {
							urlAttrMap[baseURL] = attr
						}
					}
					u.Fragment = ""
					original_response.Message.Content.Parts[0] = string(r[:citation.StartIx+offset]) + " ([" + attr + "](" + u.String() + " \"" + citation.Metadata.Title + "\"))" + string(r[citation.EndIx+offset:])
					r = []rune(original_response.Message.Content.Parts[0].(string))
					offset += len(r) - rl
				}
			}
			response_string := ""
			if original_response.Message.Content.ContentType == "multimodal_text" {
				apiUrl := "https://chat.openai.com/backend-api/files/"
				if FILES_REVERSE_PROXY != "" {
					apiUrl = FILES_REVERSE_PROXY
				}
				imgSource = make([]string, len(original_response.Message.Content.Parts))
				var wg sync.WaitGroup
				for index, part := range original_response.Message.Content.Parts {
					jsonItem, _ := json.Marshal(part)
					var dalle_content chatgpt_types.DalleContent
					err = json.Unmarshal(jsonItem, &dalle_content)
					if err != nil {
						continue
					}
					url := apiUrl + strings.Split(dalle_content.AssetPointer, "//")[1] + "/download"
					wg.Add(1)
					go GetImageSource(&wg, url, dalle_content.Metadata.Dalle.Prompt, secret, deviceId, index, imgSource)
				}
				wg.Wait()
				translated_response := official_types.NewChatCompletionChunk(strings.Join(imgSource, "") + "\n")
				if isRole {
					translated_response.Choices[0].Delta.Role = original_response.Message.Author.Role
				}
				response_string = "data: " + translated_response.String() + "\n\n"
			}
			if response_string == "" {
				response_string = chatgpt_response_converter.ConvertToString(&original_response, &previous_text, isRole)
			}
			if response_string == "" {
				if isEnd {
					goto endProcess
				} else {
					continue
				}
			}
			isRole = false
			if stream {
				_, err = c.Writer.WriteString(response_string)
				if err != nil {
					return "", nil
				}
			}
		endProcess:
			// Flush the response writer buffer to ensure that the client receives each line as it's written
			c.Writer.Flush()

			if original_response.Message.Metadata.FinishDetails != nil {
				if original_response.Message.Metadata.FinishDetails.Type == "max_tokens" {
					max_tokens = true
				}
				finish_reason = original_response.Message.Metadata.FinishDetails.Type
			}
			if isEnd {
				if stream {
					final_line := official_types.StopChunk(finish_reason)
					c.Writer.WriteString("data: " + final_line.String() + "\n\n")
				}
				break
			}
		}
	}
	if !max_tokens {
		return strings.Join(imgSource, "") + "\n" + previous_text.Text, nil
	}
	return strings.Join(imgSource, "") + "\n" + previous_text.Text, &ContinueInfo{
		ConversationID: original_response.ConversationID,
		ParentID:       original_response.Message.ID,
	}
}

package chatgpt

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"freechatgpt/internal/tokens"
	"freechatgpt/typings"
	chatgpt_types "freechatgpt/typings/chatgpt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "time/tzdata"

	"github.com/PuerkitoBio/goquery"
	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/sha3"

	chatgpt_response_converter "freechatgpt/conversion/response/chatgpt"

	official_types "freechatgpt/typings/official"
)

var (
	client              tls_client.HttpClient
	hostURL, _          = url.Parse("https://chatgpt.com")
	API_REVERSE_PROXY   = os.Getenv("API_REVERSE_PROXY")
	FILES_REVERSE_PROXY = os.Getenv("FILES_REVERSE_PROXY")
	userAgent           = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	startTime           = time.Now()
	timeLocation, _     = time.LoadLocation("Asia/Shanghai")
	timeLayout          = "Mon Jan 2 2006 15:04:05"
	cachedHardware      = 0
	cachedSid           = uuid.NewString()
	cachedScripts       = []string{}
	cachedDpl           = ""
	cachedRequireProof  = ""
)

func init() {
	cores := []int{8, 12, 16, 24}
	screens := []int{3000, 4000, 6000}
	rand.New(rand.NewSource(time.Now().UnixNano()))
	core := cores[rand.Intn(4)]
	rand.New(rand.NewSource(time.Now().UnixNano()))
	screen := screens[rand.Intn(3)]
	cachedHardware = core + screen

	envClientProfileStr := os.Getenv("CLIENT_PROFILE")
	var clientProfile profiles.ClientProfile
	if profile, ok := profiles.MappedTLSClients[envClientProfileStr]; ok {
		clientProfile = profile
	} else {
		clientProfile = profiles.Okhttp4Android13
	}
	envUserAgent := os.Getenv("UA")
	if envUserAgent != "" {
		userAgent = envUserAgent
	}
	client, _ = tls_client.NewHttpClient(tls_client.NewNoopLogger(), []tls_client.HttpClientOption{
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithTimeoutSeconds(600),
		tls_client.WithClientProfile(clientProfile),
	}...)
}

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

func SetOAICookie(uuid string) {
	client.GetCookieJar().SetCookies(hostURL, []*http.Cookie{{
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
func GetDpl(proxy string) {
	if len(cachedScripts) > 0 {
		return
	}
	if proxy != "" {
		client.SetProxy(proxy)
	}
	cachedScripts = append(cachedScripts, "https://cdn.oaistatic.com/_next/static/chunks/9598-0150caea9526d55d.js?dpl=abad631f183104e6c8a323392d7bc30b933c5c7c")
	cachedDpl = "dpl=abad631f183104e6c8a323392d7bc30b933c5c7c"
	request, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/?oai-dm=1", nil)
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "*/*")
	if err != nil {
		return
	}
	response, err := client.Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	doc, _ := goquery.NewDocumentFromReader(response.Body)
	scripts := []string{}
	inited := false
	doc.Find("script[src]").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if exists {
			scripts = append(scripts, src)
			if !inited {
				idx := strings.Index(src, "dpl")
				if idx >= 0 {
					cachedDpl = src[idx:]
					inited = true
				}
			}
		}
	})
	if len(scripts) != 0 {
		cachedScripts = scripts
	}
}
func getConfig() []interface{} {
	rand.New(rand.NewSource(time.Now().UnixNano()))
	script := cachedScripts[rand.Intn(len(cachedScripts))]
	timeNum := (float64(time.Since(startTime).Nanoseconds()) + rand.Float64()) / 1e6
	return []interface{}{cachedHardware, getParseTime(), int64(4294705152), 0, userAgent, script, cachedDpl, "en-US", "en-US", 0, "webkitGetUserMedia−function webkitGetUserMedia() { [native code] }", "location", "ontransitionend", timeNum, cachedSid}
}
func CalcProofToken(require *ChatRequire, proxy string) string {
	proof := generateAnswer(require.Proof.Seed, require.Proof.Difficulty, proxy)
	return "gAAAAAB" + proof
}

func generateAnswer(seed string, diff string, proxy string) string {
	GetDpl(proxy)
	config := getConfig()
	diffLen := len(diff)
	hasher := sha3.New512()
	for i := 0; i < 500000; i++ {
		config[3] = i
		config[9] = (i + 2) / 2
		json, _ := json.Marshal(config)
		base := base64.StdEncoding.EncodeToString(json)
		hasher.Write([]byte(seed + base))
		hash := hasher.Sum(nil)
		hasher.Reset()
		if hex.EncodeToString(hash[:diffLen])[:diffLen] <= diff {
			return base
		}
	}
	return "wQ8Lk5FbGpA2NcR9dShT6gYjU7VxZ4D" + base64.StdEncoding.EncodeToString([]byte(`"`+seed+`"`))
}

type ChatRequire struct {
	Token  string    `json:"token"`
	Proof  ProofWork `json:"proofofwork,omitempty"`
	Arkose struct {
		Required bool   `json:"required"`
		DX       string `json:"dx,omitempty"`
	} `json:"arkose"`
	Turnstile struct {
		Required bool   `json:"required"`
		DX       string `json:"dx,omitempty"`
	} `json:"turnstile"`
	ForceLogin bool `json:"force_login,omitempty"`
}

func CheckRequire(secret *tokens.Secret, deviceId string, proxy string) (*ChatRequire, string) {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	if cachedRequireProof == "" {
		cachedRequireProof = "gAAAAAC" + generateAnswer(strconv.FormatFloat(rand.Float64(), 'f', -1, 64), "0", proxy)
	}
	body := bytes.NewBuffer([]byte(`{"p":"` + cachedRequireProof + `"}`))
	var apiUrl string
	if secret.Token == "" {
		apiUrl = "https://chatgpt.com/backend-anon/sentinel/chat-requirements"
	} else {
		apiUrl = "https://chatgpt.com/backend-api/sentinel/chat-requirements"
	}
	request, err := newRequest(http.MethodPost, apiUrl, body, secret, deviceId)
	if err != nil {
		return nil, ""
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, ""
	}
	defer response.Body.Close()
	var require ChatRequire
	err = json.NewDecoder(response.Body).Decode(&require)
	if err != nil {
		return nil, ""
	}
	if require.ForceLogin {
		return nil, ""
	}
	return &require, cachedRequireProof
}

var urlAttrMap = make(map[string]string)

type urlAttr struct {
	Url         string `json:"url"`
	Attribution string `json:"attribution"`
}

func getURLAttribution(secret *tokens.Secret, deviceId string, url string) string {
	request, err := newRequest(http.MethodPost, "https://chatgpt.com/backend-api/attributions", bytes.NewBuffer([]byte(`{"urls":["`+url+`"]}`)), secret, deviceId)
	if err != nil {
		return ""
	}
	request.Header.Set("Content-Type", "application/json")
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

func POSTconversation(message ChatGPTRequest, secret *tokens.Secret, deviceId string, chat_token string, arkoseToken string, proofToken string, turnstileToken string, proxy string) (*http.Response, error) {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	var apiUrl string
	if secret.Token == "" {
		apiUrl = "https://chatgpt.com/backend-anon/conversation"
	} else {
		apiUrl = "https://chatgpt.com/backend-api/conversation"
	}
	if API_REVERSE_PROXY != "" {
		apiUrl = API_REVERSE_PROXY
	}
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
	if turnstileToken != "" {
		request.Header.Set("Openai-Sentinel-Turnstile-Token", turnstileToken)
	}
	request.Header.Set("Origin", "https://chatgpt.com")
	request.Header.Set("Referer", "https://chatgpt.com/")
	response, err := client.Do(request)
	if err != nil {
		return &http.Response{}, err
	}
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

func Handler(c *gin.Context, response *http.Response, secret *tokens.Secret, proxy string, deviceId string, uuid string, stream bool) (string, *ContinueInfo) {
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
	var imgSource []string
	var convId string
	var msgId string

	for {
		var line string
		var err error
		line, err = reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", nil
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
			if original_response.Message.Content.ContentType == "text" && original_response.Message.ID != msgId {
				if msgId == "" && original_response.Message.Content.Parts[0].(string) == "" {
					msgId = original_response.Message.ID
				} else {
					continue
				}
			}
			if original_response.Message.EndTurn != nil && !original_response.Message.EndTurn.(bool) {
				msgId = ""
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
				apiUrl := "https://chatgpt.com/backend-api/files/"
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
			if isRole && response_string != "" {
				isRole = false
			}
			if stream && response_string != "" {
				_, err = c.Writer.WriteString(response_string)
				if err != nil {
					return "", nil
				}
			}
			// Flush the response writer buffer to ensure that the client receives each line as it's written
			c.Writer.Flush()

			if original_response.Message.Metadata.FinishDetails != nil {
				if original_response.Message.Metadata.FinishDetails.Type == "max_tokens" {
					max_tokens = true
				}
				finish_reason = original_response.Message.Metadata.FinishDetails.Type
			}
		} else {
			if stream {
				final_line := official_types.StopChunk(finish_reason)
				c.Writer.WriteString("data: " + final_line.String() + "\n\n")
			}
		}
	}
	respText := strings.Join(imgSource, "")
	if respText != "" {
		respText += "\n"
	}
	respText += previous_text.Text
	if !max_tokens {
		return respText, nil
	}
	return respText, &ContinueInfo{
		ConversationID: original_response.ConversationID,
		ParentID:       original_response.Message.ID,
	}
}

func HandlerTTS(response *http.Response, input string) (string, string) {
	// Create a bufio.Reader from the response body
	reader := bufio.NewReader(response.Body)

	var original_response chatgpt_types.ChatGPTResponse
	var convId string

	for {
		var line string
		var err error
		line, err = reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", ""
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
				return "", ""
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
			if original_response.Message.Author.Role == "assistant" && original_response.Message.Content.Parts[0].(string) == input {
				return original_response.Message.ID, convId
			}
		}
	}
	return "", ""
}

func GetTTS(secret *tokens.Secret, deviceId string, url string, proxy string) []byte {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	request, err := newRequest(http.MethodGet, url, nil, secret, deviceId)
	if err != nil {
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	blob, err := io.ReadAll(response.Body)
	if err != nil {
		return nil
	}
	return blob
}

func generateRandomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	rand.New(rand.NewSource(time.Now().UnixNano()))
	bytes := make([]byte, n)
	for i := range bytes {
		bytes[i] = letters[rand.Intn(len(letters))]
	}
	return string(bytes)
}

func GetSTT(file multipart.File, header *multipart.FileHeader, lang string, secret *tokens.Secret, deviceId string, proxy string) []byte {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	boundary := "----WebKitFormBoundary" + generateRandomString(16)
	w.SetBoundary(boundary)
	part, err := w.CreatePart(header.Header)
	if err != nil {
		return nil
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return nil
	}
	if lang != "" {
		part, err := w.CreateFormField("language")
		if err != nil {
			return nil
		}
		part.Write([]byte(lang))
	}
	w.Close()
	request, err := newRequest(http.MethodPost, "https://chatgpt.com/backend-api/transcribe", &b, secret, deviceId)
	request.Header.Set("Content-Type", w.FormDataContentType())
	if err != nil {
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil
	}
	return body
}

func RemoveConversation(secret *tokens.Secret, deviceId string, id string, proxy string) {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	url := "https://chatgpt.com/backend-api/conversation/" + id
	request, err := newRequest(http.MethodPatch, url, bytes.NewBuffer([]byte(`{"is_visible":false}`)), secret, deviceId)
	request.Header.Set("Content-Type", "application/json")
	if err != nil {
		return
	}
	response, err := client.Do(request)
	if err != nil {
		return
	}
	response.Body.Close()
}

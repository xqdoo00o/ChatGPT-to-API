package chatgpt

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"freechatgpt/typings"
	chatgpt_types "freechatgpt/typings/chatgpt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"

	hp "net/http"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/gin-gonic/gin"

	chatgpt_response_converter "freechatgpt/conversion/response/chatgpt"

	official_types "freechatgpt/typings/official"
)

var (
	client, _ = tls_client.NewHttpClient(tls_client.NewNoopLogger(), []tls_client.HttpClientOption{
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithTimeoutSeconds(600),
		tls_client.WithClientProfile(profiles.Okhttp4Android13),
	}...)
	API_REVERSE_PROXY   = os.Getenv("API_REVERSE_PROXY")
	FILES_REVERSE_PROXY = os.Getenv("FILES_REVERSE_PROXY")
	conn                *websocket.Conn
)

// POSTconversation function sends a POST request to the OpenAI API to start a conversation
func POSTconversation(message chatgpt_types.ChatGPTRequest, access_token string, puid string, proxy string) (*http.Response, error) {
	// If a proxy is provided, set it for the client
	if proxy != "" {
		client.SetProxy(proxy)
	}

	// Define the API URL
	apiUrl := "https://chat.openai.com/backend-api/conversation"
	// If a reverse proxy is set, use it as the API URL
	if API_REVERSE_PROXY != "" {
		apiUrl = API_REVERSE_PROXY
	}

	// Convert the message to JSON format
	body_json, err := json.Marshal(message)
	if err != nil {
		// If an error occurs during conversion, return an empty response and the error
		return &http.Response{}, err
	}

	// Create a new POST request with the API URL and the JSONified message
	request, err := http.NewRequest(http.MethodPost, apiUrl, bytes.NewBuffer(body_json))
	if err != nil {
		// If an error occurs during request creation, return an empty response and the error
		return &http.Response{}, err
	}
	// If PUID is not provided, check the environment
	if puid == "" {
		puid = os.Getenv("PUID")
	}
	if puid != "" {
		request.Header.Set("Cookie", "_puid="+puid+";")
	}
	// Set the necessary headers for the request
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "text/event-stream")
	// If an access token is provided, add it to the request headers
	if message.Model == "gpt-4" {
		request.Header.Set("Openai-Sentinel-Arkose-Token", message.ArkoseToken)
	}
	if access_token != "" {
		request.Header.Set("Authorization", "Bearer "+access_token)
	}
	// If an error occurs during header setting, return an empty response and the error
	if err != nil {
		return &http.Response{}, err
	}
	// Send the request and get the response
	response, err := client.Do(request)
	// Return the response and any error that occurred
	return response, err
}

// Returns whether an error was handled
func Handle_request_error(c *gin.Context, response *http.Response) bool {
	// Check if the status code of the response is not 200 (OK)
	if response.StatusCode != 200 {
		// Try to read the response body as JSON into a map
		var error_response map[string]interface{}
		err := json.NewDecoder(response.Body).Decode(&error_response)
		// If there was an error reading the response body as JSON
		if err != nil {
			// Read the response body as a string
			body, _ := io.ReadAll(response.Body)
			// Send a JSON response with status code 500 (Internal Server Error) and the error details
			c.JSON(500, gin.H{"error": gin.H{
				"message": "Unknown error",
				"type":    "internal_server_error",
				"param":   nil,
				"code":    "500",
				"details": string(body),
			}})
			// Return true indicating that an error was handled
			return true
		}
		// Send a JSON response with the original status code and the error details
		c.JSON(response.StatusCode, gin.H{"error": gin.H{
			"message": error_response["detail"],
			"type":    response.Status,
			"param":   nil,
			"code":    "error",
		}})
		// Return true indicating that an error was handled
		return true
	}
	// If the status code of the response is 200 (OK), return false indicating that no error was handled
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

// GetImageSource function retrieves the source of an image from a given URL
func GetImageSource(wg *sync.WaitGroup, url string, prompt string, token string, puid string, idx int, imgSource []string) {
	// Notify the WaitGroup that this function has completed once it returns
	defer wg.Done()

	// Create a new GET request to the provided URL
	request, err := http.NewRequest(http.MethodGet, url, nil)
	// If there was an error creating the request, return immediately
	if err != nil {
		return
	}

	// If a PUID is provided, add it to the request headers
	if puid != "" {
		request.Header.Set("Cookie", "_puid="+puid+";")
	}

	// Set the necessary headers for the request
	request.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36")
	request.Header.Set("Accept", "*/*")

	// If a token is provided, add it to the request headers
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	// Send the request and get the response
	response, err := client.Do(request)
	// If there was an error sending the request, return immediately
	if err != nil {
		return
	}

	// Ensure the response body is closed once this function returns
	defer response.Body.Close()

	// Define a fileInfo struct to hold the response data
	var file_info fileInfo

	// Decode the response body into the fileInfo struct
	err = json.NewDecoder(response.Body).Decode(&file_info)

	// If there was an error decoding the response body, or if the status is not "success", return immediately
	if err != nil || file_info.Status != "success" {
		return
	}

	// Set the image source at the given index to the download URL from the response
	//imgSource[idx] = "[![image](" + file_info.DownloadURL + " \"" + prompt + "\")](" + file_info.DownloadURL + ")"
}

func Handler(c *gin.Context, response *http.Response, token string, puid string, translated_request chatgpt_types.ChatGPTRequest, stream bool) (string, *ContinueInfo) {
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
	var waitSource = false
	var isEnd = false
	var imgSource []string
	var isWSS = false
	var convId string

	firstStr, _ := reader.ReadString('\n')
	if strings.Contains(firstStr, "\"wss_url\"") {
		isWSS = true
		var wssResponse chatgpt_types.ChatGPTWSSResponse
		json.Unmarshal([]byte(firstStr), &wssResponse)
		convId = wssResponse.ConversationId
		wssUrl := wssResponse.WssUrl
		header := make(hp.Header)
		header.Add("Sec-WebSocket-Protocol", "json.reliable.webpubsub.azure.v1")
		var err error
		conn, _, err = websocket.DefaultDialer.Dial(wssUrl, header)
		if err != nil {
			return "", nil
		}

	} else {
		err := json.Unmarshal([]byte(firstStr[6:]), &original_response)
		if err != nil {
			return "", nil
		}
		if original_response.Error != nil {
			c.JSON(500, gin.H{"error": original_response.Error})
			return "", nil
		}
		convId = original_response.ConversationID
	}
	for {
		var line string
		var err error
		if isWSS {
			var messageType int
			var message []byte
			messageType, message, err = conn.ReadMessage()
			if err != nil {
				println(err.Error())
				conn.Close()
				break
			}
			if messageType == websocket.TextMessage {
				var wssMsgResponse chatgpt_types.WSSMsgResponse
				json.Unmarshal(message, &wssMsgResponse)
				base64Body := wssMsgResponse.Data.Body
				bodyByte, err := base64.StdEncoding.DecodeString(base64Body)
				if err != nil {
					continue
				}
				line = string(bodyByte)
			}
		} else {
			if firstStr != "" {
				line = firstStr
				firstStr = ""
			} else {
				line, err = reader.ReadString('\n')
			}
			if err != nil {
				if err == io.EOF {
					break
				}
				return "", nil
			}
		}
		
		// If the line length is less than 6, continue to the next iteration
		if len(line) < 6 {
			continue
		}
		
		// Remove "data: " from the beginning of the line
		line = line[6:]
		
		// Check if line starts with [DONE]
		if !strings.HasPrefix(line, "[DONE]") {
			// Parse the line as JSON
			err = json.Unmarshal([]byte(line), &original_response)
			if err != nil {
				continue
			}
			// If the original response contains an error, return the error in the response and return an empty string and nil
			if original_response.Error != nil {
				c.JSON(500, gin.H{"error": original_response.Error})
				return "", nil
			}
			// If the original response doesn't meet certain conditions, continue to the next iteration
			if original_response.ConversationID != convId {
				continue
			}
			if !(original_response.Message.Author.Role == "assistant" || (original_response.Message.Author.Role == "tool" && original_response.Message.Content.ContentType != "text")) || original_response.Message.Content.Parts == nil {
				continue
			}
			// If the original response doesn't meet certain conditions, continue to the next iteration
			if original_response.Message.Metadata.MessageType != "next" && original_response.Message.Metadata.MessageType != "continue" || !strings.HasSuffix(original_response.Message.Content.ContentType, "text") {
				continue
			}
			// If the original response has an EndTurn field, process it
			if original_response.Message.EndTurn != nil {
				if waitSource {
					waitSource = false
				}
				isEnd = true
			}
			// If the original response has citations, process them
			if len(original_response.Message.Metadata.Citations) != 0 {
				r := []rune(original_response.Message.Content.Parts[0].(string))
				if waitSource {
					if string(r[len(r)-1:]) == "】" {
						waitSource = false
					} else {
						continue
					}
				}
				offset := 0
				for i, citation := range original_response.Message.Metadata.Citations {
					rl := len(r)
					original_response.Message.Content.Parts[0] = string(r[:citation.StartIx+offset]) + "[^" + strconv.Itoa(i+1) + "^](" + citation.Metadata.URL + " \"" + citation.Metadata.Title + "\")" + string(r[citation.EndIx+offset:])
					r = []rune(original_response.Message.Content.Parts[0].(string))
					offset += len(r) - rl
				}
			} else if waitSource {
				continue
			}
			// Initialize the response_string
			response_string := ""

			// If the recipient of the original response is not "all", continue to the next iteration
			if original_response.Message.Recipient != "all" {
				continue
			}

			// If the content type of the original response is "multimodal_text", process it
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
					go GetImageSource(&wg, url, dalle_content.Metadata.Dalle.Prompt, token, puid, index, imgSource)
				}
				wg.Wait()
				translated_response := official_types.NewChatCompletionChunk(strings.Join(imgSource, ""))
				if isRole {
					translated_response.Choices[0].Delta.Role = original_response.Message.Author.Role
				}
				response_string = "data: " + translated_response.String() + "\n\n"
			}

			// If the response_string is still empty, convert the original response to a string
			if response_string == "" {
				response_string = chatgpt_response_converter.ConvertToString(&original_response, &previous_text, isRole)
			}
			
			// If the response_string is still empty, continue to the next iteration
			if response_string == "" {
				continue
			}
			// If the response_string is "【", set waitSource to true and continue to the next iteration
			if response_string == "【" {
				waitSource = true
				continue
			}
			isRole = false
			// If stream is true, write the response_string to the response writer
			if stream {
				_, err = c.Writer.WriteString(response_string)
				if err != nil {
					return "", nil
				}
			}
			// Flush the response writer buffer to ensure that the client receives each line as it's written
			c.Writer.Flush()

			// If the original response has FinishDetails, process them
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
				if isWSS {
					conn.Close()
				}
				break
			}
		}
	}
	// If max_tokens is false, return the joined imgSource and the previous text, and nil
	if !max_tokens {
		return strings.Join(imgSource, "") + previous_text.Text, nil
	}
	// If max_tokens is true, return the joined imgSource and the previous text, and a new ContinueInfo
	return strings.Join(imgSource, "") + previous_text.Text, &ContinueInfo{
		ConversationID: original_response.ConversationID,
		ParentID:       original_response.Message.ID,
	}
}

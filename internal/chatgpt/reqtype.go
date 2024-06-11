package chatgpt

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"freechatgpt/internal/tokens"
	"image"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	// 确保导入以下包以支持常见的图像格式
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	http "github.com/bogdanfinn/fhttp"

	"github.com/google/uuid"
)

type chatgpt_message struct {
	ID       uuid.UUID         `json:"id"`
	Author   chatgpt_author    `json:"author"`
	Content  chatgpt_content   `json:"content"`
	Metadata *Chatgpt_metadata `json:"metadata,omitempty"`
}

type Chatgpt_metadata struct {
	Attachments []ImgMeta `json:"attachments,omitempty"`
}

type chatgpt_content struct {
	ContentType string        `json:"content_type"`
	Parts       []interface{} `json:"parts"`
}

type chatgpt_author struct {
	Role string `json:"role"`
}
type Image_url struct {
	Url string `json:"url"`
}
type Original_multimodel struct {
	Type  string    `json:"type"`
	Text  string    `json:"text,omitempty"`
	Image Image_url `json:"image_url,omitempty"`
}

type ChatGPTConvMode struct {
	Kind    string `json:"kind"`
	GizmoId string `json:"gizmo_id,omitempty"`
}
type ChatGPTRequest struct {
	Action                     string            `json:"action"`
	ConversationMode           ChatGPTConvMode   `json:"conversation_mode"`
	Messages                   []chatgpt_message `json:"messages,omitempty"`
	ParentMessageID            string            `json:"parent_message_id,omitempty"`
	ConversationID             string            `json:"conversation_id,omitempty"`
	Model                      string            `json:"model"`
	HistoryAndTrainingDisabled bool              `json:"history_and_training_disabled"`
	WebsocketRequestId         string            `json:"websocket_request_id"`
	ForceSSE                   bool              `json:"force_use_sse"`
}
type FileResp struct {
	File_id    string `json:"file_id"`
	Status     string `json:"status"`
	Upload_url string `json:"upload_url"`
}
type UploadResp struct {
	Status       string `json:"status"`
	Download_url string `json:"download_url"`
}

type FileResult struct {
	Mime      string
	Filename  string
	Fileid    string
	Filesize  int
	Isimage   bool
	Bounds    [2]int
	TokenSize int
	// Current file max-age 30 days
	Upload int64
}

type ImgPart struct {
	Asset_pointer string `json:"asset_pointer"`
	Content_type  string `json:"content_type"`
	Size_bytes    int    `json:"size_bytes"`
	Width         int    `json:"width,omitempty"`
	Height        int    `json:"height,omitempty"`
}

type ImgMeta struct {
	Id        string `json:"id"`
	MimeType  string `json:"mimeType"`
	Name      string `json:"name"`
	Size      int    `json:"size"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	TokenSize int    `json:"file_token_size,omitempty"`
}

type RetrievalResult struct {
	FileSizeTokens int    `json:"file_size_tokens,omitempty"`
	Status         string `json:"retrieval_index_status"`
}

var (
	fileHashPool  = map[string]*FileResult{}
	retrievalMime = map[string]bool{}
)

func init() {
	// from https://chatgpt.com/backend-api/models?history_and_training_disabled=false models product_features
	retrievalMime = map[string]bool{"text/rtf": true, "application/javascript": true, "text/x-tex": true, "text/css": true, "text/xml": true, "message/rfc822": true, "text/javascript": true, "application/rtf": true, "text/x-typescript": true, "application/x-powershell": true, "application/x-sql": true, "text/x-shellscript": true, "text/x-c++": true, "text/markdown": true, "text/x-php": true, "text/x-script.python": true, "text/vbscript": true, "text/x-asm": true, "application/vnd.oasis.opendocument.text": true, "text/x-lisp": true, "application/vnd.openxmlformats-officedocument.wordprocessingml.document": true, "application/x-rust": true, "text/x-diff": true, "text/x-python": true, "application/vnd.apple.keynote": true, "application/vnd.ms-powerpoint": true, "application/x-yaml": true, "application/msword": true, "application/x-scala": true, "text/plain": true, "text/html": true, "application/json": true, "text/calendar": true, "text/x-csharp": true, "text/x-rst": true, "text/x-java": true, "text/x-makefile": true, "application/pdf": true, "text/x-c": true, "text/x-vcard": true, "application/vnd.apple.pages": true, "application/vnd.openxmlformats-officedocument.presentationml.presentation": true, "text/x-ruby": true, "text/x-sh": true}
	// from https://chromium.googlesource.com/chromium/src/+/HEAD/net/base/mime_util.cc
	mimeMap := map[string]string{
		"webm":                "video/webm",
		"mp3":                 "audio/mpeg",
		"wasm":                "application/wasm",
		"crx":                 "application/x-chrome-extension",
		"xhtml,xht,xhtm":      "application/xhtml+xml",
		"flac":                "audio/flac",
		"ogg,oga,opus":        "audio/ogg",
		"wav":                 "audio/wav",
		"m4a":                 "audio/x-m4a",
		"avif":                "image/avif",
		"gif":                 "image/gif",
		"jpeg,jpg":            "image/jpeg",
		"png":                 "image/png",
		"png,apng":            "image/apng",
		"svg,svgz":            "image/svg+xml",
		"webp":                "image/webp",
		"mht,mhtml":           "multipart/related",
		"css":                 "text/css",
		"html,htm,shtml,shtm": "text/html",
		"js,mjs":              "text/javascript",
		"xml":                 "text/xml",
		"mp4,m4v":             "video/mp4",
		"ogv,ogm":             "video/ogg",
		"csv":                 "text/csv",
		"ico":                 "image/x-icon",
		"epub":                "application/epub+zip",
		"woff":                "application/font-woff",
		"gz,tgz":              "application/gzip",
		"js":                  "application/javascript",
		"json":                "application/json",
		"doc,dot":             "application/msword",
		"bin,exe,com":         "application/octet-stream",
		"pdf":                 "application/pdf",
		"p7m,p7c,p7z":         "application/pkcs7-mime",
		"p7s":                 "application/pkcs7-signature",
		"ps,eps,ai":           "application/postscript",
		"rdf":                 "application/rdf+xml",
		"rss":                 "application/rss+xml",
		"rtf":                 "application/rtf",
		"apk":                 "application/vnd.android.package-archive",
		"xul":                 "application/vnd.mozilla.xul+xml",
		"xls":                 "application/vnd.ms-excel",
		"ppt":                 "application/vnd.ms-powerpoint",
		"pptx":                "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"xlsx":                "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"docx":                "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"m3u8":                "application/x-mpegurl",
		"swf,swl":             "application/x-shockwave-flash",
		"tar":                 "application/x-tar",
		"cer,crt":             "application/x-x509-ca-cert",
		"zip":                 "application/zip",
		"weba":                "audio/webm",
		"bmp":                 "image/bmp",
		"jfif,pjpeg,pjp":      "image/jpeg",
		"tiff,tif":            "image/tiff",
		"xbm":                 "image/x-xbitmap",
		"eml":                 "message/rfc822",
		"ics":                 "text/calendar",
		"ehtml":               "text/html",
		"txt,text":            "text/plain",
		"sh":                  "text/x-sh",
		"xsl,xbl,xslt":        "text/xml",
		"mpeg,mpg":            "video/mpeg",
	}
	for key, item := range mimeMap {
		keyArr := strings.Split(key, ",")
		if len(keyArr) == 1 {
			mime.AddExtensionType("."+key, item)
		} else {
			for _, ext := range keyArr {
				mime.AddExtensionType("."+ext, item)
			}
		}
	}
	file, err := os.Open("fileHashes.json")
	if err != nil {
		return
	}
	defer file.Close()
	err = json.NewDecoder(file).Decode(&fileHashPool)
	if err != nil {
		return
	}
}
func SaveFileHash() {
	if len(fileHashPool) == 0 {
		return
	}
	file, err := os.OpenFile("fileHashes.json", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	err = json.NewEncoder(file).Encode(fileHashPool)
	if err != nil {
		return
	}
}
func NewChatGPTRequest() ChatGPTRequest {
	disable_history := os.Getenv("ENABLE_HISTORY") != "true"
	return ChatGPTRequest{
		Action:                     "next",
		ParentMessageID:            uuid.NewString(),
		Model:                      "text-davinci-002-render-sha",
		HistoryAndTrainingDisabled: disable_history,
		ConversationMode:           ChatGPTConvMode{Kind: "primary_assistant"},
		WebsocketRequestId:         uuid.NewString(),
		ForceSSE:                   true,
	}
}

func processUrl(urlstr string, account string, secret *tokens.Secret, deviceId string, proxy string) *FileResult {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	u, err := url.Parse(urlstr)
	if err != nil {
		return nil
	}
	fileName := path.Base(u.Path)
	extIndex := strings.Index(fileName, ".")
	mimeType := mime.TypeByExtension(fileName[extIndex:])
	mimeIdx := strings.Index(mimeType, ";")
	if mimeIdx != -1 {
		mimeType = mimeType[:mimeIdx]
	}
	request, err := http.NewRequest(http.MethodGet, urlstr, nil)
	if err != nil {
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	binary, err := io.ReadAll(response.Body)
	if err != nil {
		return nil
	}
	hasher := sha1.New()
	hasher.Write(binary)
	hash := account + secret.TeamUserID + hex.EncodeToString(hasher.Sum(nil))
	if fileHashPool[hash] != nil && time.Now().Unix() < fileHashPool[hash].Upload+2592000 {
		return fileHashPool[hash]
	}
	isImg := strings.HasPrefix(mimeType, "image")
	var bounds [2]int
	if isImg {
		img, _, _ := image.Decode(bytes.NewReader(binary))
		if img != nil {
			bounds[0] = img.Bounds().Dx()
			bounds[1] = img.Bounds().Dy()
		}
	}
	fileid := uploadBinary(binary, mimeType, fileName, isImg, secret, deviceId, proxy)
	if fileid == "" {
		return nil
	} else {
		tokenSize := 0
		if !isImg && retrievalMime[mimeType] {
			tokenSize = getRetrievalToken(fileid, 10, secret, deviceId, proxy)
		}
		result := FileResult{Mime: mimeType, Filename: fileName, Filesize: len(binary), Fileid: fileid, Isimage: isImg, Bounds: bounds, TokenSize: tokenSize, Upload: time.Now().Unix()}
		fileHashPool[hash] = &result
		return &result
	}
}
func processDataUrl(data string, account string, secret *tokens.Secret, deviceId string, proxy string) *FileResult {
	commaIndex := strings.Index(data, ",")
	binary, err := base64.StdEncoding.DecodeString(data[commaIndex+1:])
	if err != nil {
		return nil
	}
	hasher := sha1.New()
	hasher.Write(binary)
	hash := account + secret.TeamUserID + hex.EncodeToString(hasher.Sum(nil))
	if fileHashPool[hash] != nil && time.Now().Unix() < fileHashPool[hash].Upload+2592000 {
		return fileHashPool[hash]
	}
	startIdx := strings.Index(data, ":")
	endIdx := strings.Index(data, ";")
	mimeType := data[startIdx+1 : endIdx]
	mimeIdx := strings.Index(mimeType, ";")
	if mimeIdx != -1 {
		mimeType = mimeType[:mimeIdx]
	}
	var fileName string
	extensions, _ := mime.ExtensionsByType(mimeType)
	if len(extensions) > 0 {
		fileName = "file" + extensions[0]
	} else {
		index := strings.Index(mimeType, "/")
		fileName = "file." + mimeType[index+1:]
	}
	isImg := strings.HasPrefix(mimeType, "image")
	var bounds [2]int
	if isImg {
		img, _, _ := image.Decode(bytes.NewReader(binary))
		if img != nil {
			bounds[0] = img.Bounds().Dx()
			bounds[1] = img.Bounds().Dy()
		}
	}
	fileid := uploadBinary(binary, mimeType, fileName, isImg, secret, deviceId, proxy)
	if fileid == "" {
		return nil
	} else {
		tokenSize := 0
		if !isImg && retrievalMime[mimeType] {
			tokenSize = getRetrievalToken(fileid, 10, secret, deviceId, proxy)
		}
		result := FileResult{Mime: mimeType, Filename: fileName, Filesize: len(binary), Fileid: fileid, Isimage: isImg, Bounds: bounds, TokenSize: tokenSize, Upload: time.Now().Unix()}
		fileHashPool[hash] = &result
		return &result
	}
}
func uploadBinary(data []byte, mime string, name string, isImg bool, secret *tokens.Secret, deviceId string, proxy string) string {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	var fileCase string
	if isImg {
		fileCase = "multimodal"
	} else if retrievalMime[mime] {
		fileCase = "my_files"
	} else {
		fileCase = "ace_upload"
	}
	dataLen := strconv.Itoa(len(data))
	request, err := newRequest(http.MethodPost, "https://chatgpt.com/backend-api/files", bytes.NewBuffer([]byte(`{"file_name":"`+name+`","file_size":`+dataLen+`,"use_case":"`+fileCase+`"}`)), secret, deviceId)
	if err != nil {
		return ""
	}
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	var fileResp FileResp
	err = json.NewDecoder(response.Body).Decode(&fileResp)
	if err != nil {
		return ""
	}
	if fileResp.Status != "success" {
		return ""
	}
	request, err = http.NewRequest(http.MethodPut, fileResp.Upload_url, bytes.NewReader(data))
	if err != nil {
		return ""
	}
	request.Header.Set("X-Ms-Blob-Type", "BlockBlob")
	request.Header.Set("X-Ms-Version", "2020-04-08")
	response, err = client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		return ""
	}
	request, err = newRequest(http.MethodPost, "https://chatgpt.com/backend-api/files/"+fileResp.File_id+"/uploaded", bytes.NewBuffer([]byte(`{}`)), secret, deviceId)
	if err != nil {
		return ""
	}
	response, err = client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	var uploadResp UploadResp
	err = json.NewDecoder(response.Body).Decode(&uploadResp)
	if err != nil {
		return ""
	}
	return fileResp.File_id
}
func getRetrievalToken(fileid string, retry int, secret *tokens.Secret, deviceId string, proxy string) int {
	if proxy != "" {
		client.SetProxy(proxy)
	}
	request, err := newRequest(http.MethodGet, "https://chatgpt.com/backend-api/files/"+fileid, nil, secret, deviceId)
	if err != nil {
		return 0
	}
	response, err := client.Do(request)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	var evalResp RetrievalResult
	err = json.NewDecoder(response.Body).Decode(&evalResp)
	if err != nil {
		return 0
	}
	if evalResp.Status == "success" {
		return evalResp.FileSizeTokens
	} else {
		retry = retry - 1
		if retry == 0 {
			return 0
		} else {
			time.Sleep(time.Millisecond * 500)
			return getRetrievalToken(fileid, retry, secret, deviceId, proxy)
		}
	}
}
func (c *ChatGPTRequest) AddMessage(role string, content interface{}, multimodal bool, account string, secret *tokens.Secret, deviceId string, proxy string) {
	parts := []interface{}{}
	var metadatas Chatgpt_metadata
	msgType := "text"
	switch v := content.(type) {
	case string:
		parts = append(parts, v)
	case []interface{}:
		var items []Original_multimodel
		for _, item := range v {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			itemtype, _ := itemMap["type"].(string)
			if itemtype == "text" {
				text, _ := itemMap["text"].(string)
				items = append(items, Original_multimodel{Type: itemtype, Text: text})
			} else {
				imageMap, _ := itemMap["image_url"].(map[string]interface{})
				items = append(items, Original_multimodel{Type: itemtype, Image: Image_url{Url: imageMap["url"].(string)}})
			}
		}
		for _, item := range items {
			if item.Type == "image_url" {
				if !multimodal {
					continue
				}
				data := item.Image.Url
				var result *FileResult
				if strings.HasPrefix(data, "data:") {
					result = processDataUrl(data, account, secret, deviceId, proxy)
					if result == nil {
						continue
					}
				} else {
					result = processUrl(data, account, secret, deviceId, proxy)
					if result == nil {
						continue
					}
				}
				if result.Isimage {
					msgType = "multimodal_text"
					parts = append(parts, ImgPart{Asset_pointer: "file-service://" + result.Fileid, Content_type: "image_asset_pointer", Size_bytes: result.Filesize, Width: result.Bounds[0], Height: result.Bounds[1]})
					metadatas.Attachments = append(metadatas.Attachments, ImgMeta{Id: result.Fileid, Name: result.Filename, Size: result.Filesize, MimeType: result.Mime, Width: result.Bounds[0], Height: result.Bounds[1]})
				} else {
					metadatas.Attachments = append(metadatas.Attachments, ImgMeta{Id: result.Fileid, Name: result.Filename, Size: result.Filesize, MimeType: result.Mime, TokenSize: result.TokenSize})
				}
			} else {
				parts = append(parts, item.Text)
			}
		}
	}
	var msg = chatgpt_message{
		ID:       uuid.New(),
		Author:   chatgpt_author{Role: role},
		Content:  chatgpt_content{ContentType: msgType, Parts: parts},
		Metadata: nil,
	}
	if metadatas.Attachments != nil {
		msg.Metadata = &metadatas
	}
	c.Messages = append(c.Messages, msg)
}

func (c *ChatGPTRequest) AddAssistantMessage(input string) {
	var msg = chatgpt_message{
		ID:       uuid.New(),
		Author:   chatgpt_author{Role: "assistant"},
		Content:  chatgpt_content{ContentType: "text", Parts: []interface{}{input}},
		Metadata: nil,
	}
	c.Messages = append(c.Messages, msg)
}

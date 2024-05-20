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

	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
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
	Mime     string
	Filename string
	Fileid   string
	Filesize int
	Isimage  bool
	Bounds   [2]int
	// Current file max-age 1 year
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
	Id       string `json:"id"`
	MimeType string `json:"mimeType"`
	Name     string `json:"name"`
	Size     int    `json:"size"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

var (
	client, _ = tls_client.NewHttpClient(tls_client.NewNoopLogger(), []tls_client.HttpClientOption{
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithTimeoutSeconds(600),
		tls_client.WithClientProfile(profiles.Okhttp4Android13),
	}...)
	userAgent    = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	fileHashPool = map[string]*FileResult{}
)

func init() {
	u, _ := url.Parse("https://chatgpt.com")
	client.GetCookieJar().SetCookies(u, []*http.Cookie{{
		Name:  "oai-dm-tgt-c-240329",
		Value: "2024-04-02",
	}})
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
	if fileHashPool[hash] != nil && time.Now().Unix() < fileHashPool[hash].Upload+31536000 {
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
		result := FileResult{Mime: mimeType, Filename: fileName, Filesize: len(binary), Fileid: fileid, Isimage: isImg, Bounds: bounds, Upload: time.Now().Unix()}
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
	if fileHashPool[hash] != nil && time.Now().Unix() < fileHashPool[hash].Upload+31536000 {
		return fileHashPool[hash]
	}
	startIdx := strings.Index(data, ":")
	endIdx := strings.Index(data, ";")
	mimeType := data[startIdx+1 : endIdx]
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
		result := FileResult{Mime: mimeType, Filename: fileName, Filesize: len(binary), Fileid: fileid, Isimage: isImg, Bounds: bounds, Upload: time.Now().Unix()}
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

func (c *ChatGPTRequest) AddMessage(role string, content interface{}, multimodal bool, account string, secret *tokens.Secret, deviceId string, proxy string) {
	parts := []interface{}{}
	var result *FileResult
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
					parts = append(parts, ImgPart{Asset_pointer: "file-service://" + result.Fileid, Content_type: "image_asset_pointer", Size_bytes: result.Filesize, Width: result.Bounds[0], Height: result.Bounds[1]})
				}
			} else {
				parts = append(parts, item.Text)
			}
		}
	}
	var msg = chatgpt_message{
		ID:       uuid.New(),
		Author:   chatgpt_author{Role: role},
		Content:  chatgpt_content{ContentType: "text", Parts: parts},
		Metadata: nil,
	}
	if result != nil {
		if result.Isimage {
			msg.Content.ContentType = "multimodal_text"
		}
		msg.Metadata = &Chatgpt_metadata{Attachments: []ImgMeta{{Id: result.Fileid, Name: result.Filename, Size: result.Filesize, MimeType: result.Mime, Width: result.Bounds[0], Height: result.Bounds[1]}}}
	}
	c.Messages = append(c.Messages, msg)
}

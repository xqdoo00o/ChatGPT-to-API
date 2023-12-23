package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/generative-ai-go/genai"
	openai "github.com/sashabaranov/go-openai"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"freechatgpt/internal/gemini/pkg/protocol"
	"freechatgpt/internal/gemini/pkg/util"
)

func IndexHandler(c *gin.Context) {
	c.JSON(http.StatusMisdirectedRequest, gin.H{
		"message": "Welcome to the OpenAI API! Documentation is available at https://platform.openai.com/docs/api-reference",
	})
}

func ModelListHandler(c *gin.Context) {
	model := openai.Model{
		CreatedAt: 1686935002,
		ID:        openai.GPT3Dot5Turbo,
		Object:    "model",
		OwnedBy:   "openai",
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   []any{model},
	})
}

func ModelRetrieveHandler(c *gin.Context) {
	model := openai.Model{
		CreatedAt: 1686935002,
		ID:        openai.GPT3Dot5Turbo,
		Object:    "model",
		OwnedBy:   "openai",
	}

	c.JSON(http.StatusOK, model)
}

func VisionProxyHandler(c *gin.Context) {
	// Retrieve the Authorization header value
	authorizationHeader := c.GetHeader("Authorization")
	// Declare a variable to store the OPENAI_API_KEY
	var openaiAPIKey string
	// Use fmt.Sscanf to extract the Bearer token
	_, err := fmt.Sscanf(authorizationHeader, "Bearer %s", &openaiAPIKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req openai.ChatCompletionRequest
	// Bind the JSON data from the request to the struct
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	client, err := genai.NewClient(ctx, option.WithAPIKey(openaiAPIKey))
	if err != nil {
		log.Printf("new genai client error %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer client.Close()

	texts := ""
	image := []byte{}
	for _, it := range req.Messages {
		for _, ij := range it.MultiContent {
			if strings.TrimSpace(ij.Text) != "" {
				texts += ij.Text
				continue
			}
			if ij.ImageURL == nil {
				continue
			}
			link := strings.TrimSpace(ij.ImageURL.URL)
			if link == "" {
				continue
			}
			response, err := http.Get(link)
			if err != nil {
				log.Printf("new genai client error %v\n", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			defer response.Body.Close()
			buff := &bytes.Buffer{}
			if _, err := io.Copy(buff, response.Body); err != nil {
				log.Printf("new genai client error %v\n", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			image = buff.Bytes()
		}
	}

	prompt := genai.Text(texts)
	imageData := genai.ImageData("jpeg", image)
	model := client.GenerativeModel(protocol.GeminiProVision)
	protocol.SetGenaiModelByOpenaiRequest(model, req)

	cs := model.StartChat()
	protocol.SetGenaiChatByOpenaiRequest(cs, req)

	if !req.Stream {
		genaiResp, err := cs.SendMessage(ctx, prompt, imageData)
		if err != nil {
			log.Printf("genai send message error %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		openaiResp := protocol.GenaiResponseToOpenaiResponse(genaiResp)
		c.JSON(http.StatusOK, openaiResp)
		return
	}

	iter := cs.SendMessageStream(ctx, prompt, imageData)
	dataChan := make(chan string)
	go func() {
		defer close(dataChan)

		defer func() {
			if r := recover(); r != nil {
				log.Println("Recovered. Error:\n", r)
			}
		}()

		respID := util.GetUUID()
		created := time.Now().Unix()

		for {
			genaiResp, err := iter.Next()
			if err == iterator.Done {
				break
			}

			if err != nil {
				log.Printf("genai get stream message error %v\n", err)
				dataChan <- fmt.Sprintf(`{"error": "%s"}`, err.Error())
				break
			}

			openaiResp := protocol.GenaiResponseToStreamCompletionResponse(genaiResp, respID, created)
			resp, _ := json.Marshal(openaiResp)
			dataChan <- string(resp)

			if len(openaiResp.Choices) > 0 && openaiResp.Choices[0].FinishReason != nil {
				break
			}
		}
	}()

	setEventStreamHeaders(c)
	c.Stream(func(w io.Writer) bool {
		if data, ok := <-dataChan; ok {
			c.Render(-1, protocol.Event{Data: "data: " + data})
			return true
		}
		c.Render(-1, protocol.Event{Data: "data: [DONE]"})
		return false
	})
}

func ChatProxyHandler(c *gin.Context) {
	// Retrieve the Authorization header value
	authorizationHeader := c.GetHeader("Authorization")
	// Declare a variable to store the OPENAI_API_KEY
	var openaiAPIKey string
	// Use fmt.Sscanf to extract the Bearer token
	_, err := fmt.Sscanf(authorizationHeader, "Bearer %s", &openaiAPIKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var req openai.ChatCompletionRequest
	// Bind the JSON data from the request to the struct
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request message must not be empty!"})
		return
	}

	ctx := c.Request.Context()
	client, err := genai.NewClient(ctx, option.WithAPIKey(openaiAPIKey))
	if err != nil {
		log.Printf("new genai client error %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer client.Close()

	model := client.GenerativeModel(protocol.GeminiPro)
	protocol.SetGenaiModelByOpenaiRequest(model, req)

	cs := model.StartChat()
	protocol.SetGenaiChatByOpenaiRequest(cs, req)

	prompt := genai.Text(req.Messages[len(req.Messages)-1].Content)

	if !req.Stream {
		genaiResp, err := cs.SendMessage(ctx, prompt)
		if err != nil {
			log.Printf("genai send message error %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		openaiResp := protocol.GenaiResponseToOpenaiResponse(genaiResp)
		c.JSON(http.StatusOK, openaiResp)
		return
	}

	iter := cs.SendMessageStream(ctx, prompt)
	dataChan := make(chan string)
	go func() {
		defer close(dataChan)

		defer func() {
			if r := recover(); r != nil {
				log.Println("Recovered. Error:\n", r)
			}
		}()

		respID := util.GetUUID()
		created := time.Now().Unix()

		for {
			genaiResp, err := iter.Next()
			if err == iterator.Done {
				break
			}

			if err != nil {
				log.Printf("genai get stream message error %v\n", err)
				dataChan <- fmt.Sprintf(`{"error": "%s"}`, err.Error())
				break
			}

			openaiResp := protocol.GenaiResponseToStreamCompletionResponse(genaiResp, respID, created)
			resp, _ := json.Marshal(openaiResp)
			dataChan <- string(resp)

			if len(openaiResp.Choices) > 0 && openaiResp.Choices[0].FinishReason != nil {
				break
			}
		}
	}()

	setEventStreamHeaders(c)
	c.Stream(func(w io.Writer) bool {
		if data, ok := <-dataChan; ok {
			c.Render(-1, protocol.Event{Data: "data: " + data})
			return true
		}
		c.Render(-1, protocol.Event{Data: "data: [DONE]"})
		return false
	})
}

func setEventStreamHeaders(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

package protocol

import (
	"fmt"
	"log"
	"time"

	"github.com/google/generative-ai-go/genai"
	openai "github.com/sashabaranov/go-openai"

	"github.com/zhu327/gemini-openai-proxy/pkg/util"
)

const (
	GeminiPro       = "gemini-pro"
	GeminiProVision = "gemini-pro-vision"

	genaiRoleUser  = "user"
	genaiRoleModel = "model"
)

type CompletionChoice struct {
	Index int `json:"index"`
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type CompletionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []CompletionChoice `json:"choices"`
}

func GenaiResponseToStreamCompletionResponse(
	genaiResp *genai.GenerateContentResponse,
	respID string,
	created int64,
) *CompletionResponse {
	resp := CompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", respID),
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   GeminiPro,
		Choices: make([]CompletionChoice, 0, len(genaiResp.Candidates)),
	}

	for i, candidate := range genaiResp.Candidates {
		var content string
		if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
			if s, ok := candidate.Content.Parts[0].(genai.Text); ok {
				content = string(s)
			}
		}

		choice := CompletionChoice{
			Index: i,
		}
		choice.Delta.Content = content

		if candidate.FinishReason > genai.FinishReasonStop {
			log.Printf("genai message finish reason %s\n", candidate.FinishReason.String())

			var openaiFinishReason string = string(openai.FinishReasonStop)
			if candidate.FinishReason == genai.FinishReasonMaxTokens {
				openaiFinishReason = string(openai.FinishReasonLength)
			}
			choice.FinishReason = &openaiFinishReason
		}

		resp.Choices = append(resp.Choices, choice)
	}
	return &resp
}

func GenaiResponseToOpenaiResponse(
	genaiResp *genai.GenerateContentResponse,
) openai.ChatCompletionResponse {
	resp := openai.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", util.GetUUID()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   GeminiPro,
		Choices: make([]openai.ChatCompletionChoice, 0, len(genaiResp.Candidates)),
	}

	for i, candidate := range genaiResp.Candidates {
		var content string
		if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
			if s, ok := candidate.Content.Parts[0].(genai.Text); ok {
				content = string(s)
			}
		}

		choice := openai.ChatCompletionChoice{
			Index: i,
			Message: openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: content,
			},
			FinishReason: openai.FinishReasonStop,
		}
		resp.Choices = append(resp.Choices, choice)
	}
	return resp
}

func SetGenaiChatByOpenaiRequest(cs *genai.ChatSession, req openai.ChatCompletionRequest) {
	cs.History = make([]*genai.Content, 0, len(req.Messages))
	if len(req.Messages) > 1 {
		for _, message := range req.Messages[:len(req.Messages)-1] {
			switch message.Role {
			case openai.ChatMessageRoleSystem:
				cs.History = append(cs.History, []*genai.Content{
					{
						Parts: []genai.Part{
							genai.Text(message.Content),
						},
						Role: genaiRoleUser,
					},
					{
						Parts: []genai.Part{
							genai.Text("ok."),
						},
						Role: genaiRoleModel,
					},
				}...)
			case openai.ChatMessageRoleAssistant:
				cs.History = append(cs.History, &genai.Content{
					Parts: []genai.Part{
						genai.Text(message.Content),
					},
					Role: genaiRoleModel,
				})
			case openai.ChatMessageRoleUser:
				cs.History = append(cs.History, &genai.Content{
					Parts: []genai.Part{
						genai.Text(message.Content),
					},
					Role: genaiRoleUser,
				})
			}
		}
	}

	if len(cs.History) != 0 && cs.History[len(cs.History)-1].Role != genaiRoleModel {
		cs.History = append(cs.History, &genai.Content{
			Parts: []genai.Part{
				genai.Text("ok."),
			},
			Role: genaiRoleModel,
		})
	}
}

func SetGenaiModelByOpenaiRequest(model *genai.GenerativeModel, req openai.ChatCompletionRequest) {
	if req.MaxTokens != 0 {
		maxToken := int32(req.MaxTokens)
		model.MaxOutputTokens = &maxToken
	}
	if req.Temperature != 0 {
		model.Temperature = &req.Temperature
	}
	if req.TopP != 0 {
		model.TopP = &req.TopP
	}
	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockOnlyHigh,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockOnlyHigh,
		},
	}
}

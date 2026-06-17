package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yuluo-yx/agentscope-go/agent"
	"github.com/yuluo-yx/agentscope-go/credential"
	"github.com/yuluo-yx/agentscope-go/message"
	asmodel "github.com/yuluo-yx/agentscope-go/model"
	"github.com/yuluo-yx/agentscope-go/model/dashscope"
	"github.com/yuluo-yx/agentscope-go/permission"
	asstate "github.com/yuluo-yx/agentscope-go/state"
	"github.com/yuluo-yx/agentscope-go/tool"
)

func main() {
	// Create a Gin router with default middleware (logger and recovery)
	r := gin.Default()

	// Define a simple GET endpoint
	r.GET("/ping", pong)
	r.GET("/chat", chat)
	r.GET("/stream-chat", streamChat)
	r.GET("/stream-chat-tool", streamChatTool)
	r.GET("/agent/chat", agentChat)
	r.GET("/agent/stream-chat", agentStreamChat)
	r.GET("/structured", structuredOutput)

	if err := r.Run(); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

func pong(c *gin.Context) {

	// Return JSON response
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}

// curl -v '127.0.0.1:8080/chat?prompt=hello'
func chat(c *gin.Context) {

	chatModel := newChatModel(false)
	user, err := message.NewUserMessage("user", c.Query("prompt"))
	if err != nil {
		panic(err)
	}
	chatResponse, err := chatModel.Call(c.Request.Context(), asmodel.CallRequest{
		Messages: []*message.Message{user},
	})
	if err != nil {
		panic(err)
	}

	c.JSON(http.StatusOK, gin.H{"resp:": chatResponse.Content})
}

// curl -N '127.0.0.1:8080/stream-chat?prompt=hello'
func streamChat(c *gin.Context) {

	chatModel := newChatModel(true)

	user, err := message.NewUserMessage("user", c.Query("prompt"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ch, err := chatModel.Stream(c.Request.Context(), asmodel.CallRequest{
		Messages: []*message.Message{user},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	for resp := range ch {
		if resp.Error != nil {
			data, _ := json.Marshal(gin.H{"error": resp.Error.Error()})
			fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
			c.Writer.Flush()
			return
		}
		data, _ := json.Marshal(resp.Content)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}
}

// curl -N '127.0.0.1:8080/stream-chat-tool?prompt=hello'
func streamChatTool(c *gin.Context) {
	chatModel := newChatModel(true)

	user, err := message.NewUserMessage("user", "杭州天气怎么样？")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// tool
	wt, err := weatherTool()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	toolkit, err := tool.NewToolkit(wt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	toolSchemas, err := toolkit.ToolSchemas()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ch, err := chatModel.Stream(c.Request.Context(), asmodel.CallRequest{
		Messages: []*message.Message{user},
		Tools:    toolSchemas,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	var final *asmodel.ChatResponse
	for resp := range ch {
		if resp.Error != nil {
			data, _ := json.Marshal(gin.H{"error": resp.Error.Error()})
			fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
			c.Writer.Flush()
			return
		}
		if resp.IsLast {
			final = resp.Clone()
		}
		data, _ := json.Marshal(resp.Content)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}
	var weatherCall *message.ToolCallBlock
	if final != nil {
		for _, block := range final.Content {
			if toolCall, ok := block.(*message.ToolCallBlock); ok {
				weatherCall = toolCall
				break
			}
		}
	}
	if weatherCall == nil {
		return
	}
	toolResponse, err := toolkit.RunTool(c.Request.Context(), weatherCall, asstate.NewAgentState())
	if err != nil {
		data, _ := json.Marshal(gin.H{"error": err.Error()})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
		c.Writer.Flush()
		return
	}
	data, _ := json.Marshal(toolResponse.Content)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	c.Writer.Flush()

	assistantMessage, err := message.NewAssistantMessage("assistant", final.Content)
	if err != nil {
		data, _ := json.Marshal(gin.H{"error": err.Error()})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
		c.Writer.Flush()
		return
	}
	toolMessage, err := message.NewAssistantMessage("tool", message.ContentBlockList{
		message.NewToolResultBlock(weatherCall.ID, weatherCall.Name, message.ToolResultOutput{Blocks: toolResponse.Content}, toolResponse.State),
	})
	if err != nil {
		data, _ := json.Marshal(gin.H{"error": err.Error()})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
		c.Writer.Flush()
		return
	}
	answer, err := chatModel.Stream(c.Request.Context(), asmodel.CallRequest{
		Messages: []*message.Message{user, assistantMessage, toolMessage},
		Tools:    toolSchemas,
	})
	if err != nil {
		data, _ := json.Marshal(gin.H{"error": err.Error()})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
		c.Writer.Flush()
		return
	}
	for resp := range answer {
		if resp.Error != nil {
			data, _ := json.Marshal(gin.H{"error": resp.Error.Error()})
			fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
			c.Writer.Flush()
			return
		}
		data, _ := json.Marshal(resp.Content)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		c.Writer.Flush()
	}
}

// curl -v '127.0.0.1:8080/agent/chat'
func agentChat(c *gin.Context) {

	wt, err := weatherTool()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	toolkit, err := tool.NewToolkit(wt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeExplore)
	agent, err := agent.NewAgent(
		"Journey Agent",
		"Use GetWeather before answering weather or travel-planning questions.",
		newChatModel(false),
		agent.WithToolkit(toolkit),
		agent.WithAgentState(state),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	user, err := message.NewUserMessage("user", "看下杭州天气，帮我规划 plan")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	reply, err := agent.Reply(c.Request.Context(), user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"resp:": reply.Content})
}

// curl -v '127.0.0.1:8080/agent/stream-chat'
func agentStreamChat(c *gin.Context) {
	wt, err := weatherTool()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	toolkit, err := tool.NewToolkit(wt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	state := asstate.NewAgentState()
	state.PermissionContext = permission.NewContext(permission.ModeExplore)
	agent, err := agent.NewAgent(
		"Journey Agent",
		"Use GetWeather before answering weather or travel-planning questions.",
		newChatModel(true),
		agent.WithToolkit(toolkit),
		agent.WithAgentState(state),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	user, err := message.NewUserMessage("user", "看下杭州天气，帮我规划 plan")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	err = agent.ReplyStream(c.Request.Context(), user, func(event message.Event) error {
		switch e := event.(type) {
		case *message.ToolCallStartEvent:
			data, _ := json.Marshal(gin.H{"tool": e.ToolCallName})
			fmt.Fprintf(c.Writer, "event: tool_call_start\ndata: %s\n\n", data)
			c.Writer.Flush()
		case *message.ToolResultEndEvent:
			data, _ := json.Marshal(gin.H{"state": e.State})
			fmt.Fprintf(c.Writer, "event: tool_result_end\ndata: %s\n\n", data)
			c.Writer.Flush()
		case *message.TextBlockDeltaEvent:
			data, _ := json.Marshal(gin.H{"delta": e.Delta})
			fmt.Fprintf(c.Writer, "event: text_delta\ndata: %s\n\n", data)
			c.Writer.Flush()
		}
		return nil
	})
	if err != nil {
		data, _ := json.Marshal(gin.H{"error": err.Error()})
		fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
		c.Writer.Flush()
		return
	}
}

// curl -v '127.0.0.1:8080/structured?prompt=Hangzhou%20day%20trip'
func structuredOutput(c *gin.Context) {
	chatModel := newChatModel(false)
	prompt := strings.TrimSpace(c.Query("prompt"))
	if prompt == "" {
		prompt = "杭州一日游"
	}
	user, err := message.NewUserMessage("user", fmt.Sprintf(`Return only valid compact JSON with this schema:
{"city":"string","weather":"string","plan":["string"],"tips":["string"]}
User request: %s`, prompt))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	chatResponse, err := chatModel.Call(c.Request.Context(), asmodel.CallRequest{
		Messages: []*message.Message{user},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	responseText := chatResponse.GetTextContent()
	if responseText == nil {
		responseText = new(string)
	}
	raw := strings.TrimSpace(*responseText)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			raw = raw[start : end+1]
		}
	}
	var structured map[string]any
	if err := json.Unmarshal([]byte(raw), &structured); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "raw": raw})
		return
	}
	c.JSON(http.StatusOK, gin.H{"resp:": structured})
}

// ========= util ===========

func newChatModel(stream bool) asmodel.ChatModel {

	chatModel, err := dashscope.NewChatModel(
		credential.NewDashScope(os.Getenv("AI_DASHSCOPE_API_KEY")).ChatCredential(),
		"qwen3.7-max",
		dashscope.WithStream(stream),
	)
	if err != nil {
		panic(err)
	}

	return chatModel
}

func weatherTool() (*tool.FunctionTool, error) {

	return tool.NewFunctionTool(
		"GetWeather",
		"Return weather for one city.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string", "description": "City name."},
			},
			"required": []any{"city"},
		},
		func(context.Context, map[string]any, *asstate.AgentState) (message.ContentBlockList, error) {

			// mock, return sunny for any city
			return message.ContentBlockList{message.NewTextBlock("sunny")}, nil
		},
		tool.WithFunctionReadOnly(true),
	)
}

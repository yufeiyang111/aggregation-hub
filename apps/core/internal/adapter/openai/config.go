package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
)

var ErrInvalidConfig = errors.New("OpenAI Compatible Adapter 配置无效")

type WireAPI string

const (
	WireAPIChatCompletions WireAPI = "chat_completions"
	WireAPIResponses       WireAPI = "responses"
)

type AuthHeaderMode string

const (
	AuthHeaderAuthorizationBearer AuthHeaderMode = "authorization_bearer"
	AuthHeaderXAPIKey             AuthHeaderMode = "x_api_key"
)

// Config 只描述非秘密、受允许字段约束的上游协议差异。
type Config struct {
	WireAPI             WireAPI        `json:"wire_api"`
	ChatCompletionsPath string         `json:"chat_completions_path"`
	ResponsesPath       string         `json:"responses_path"`
	ModelsPath          string         `json:"models_path"`
	AuthHeaderMode      AuthHeaderMode `json:"auth_header_mode"`
}

func DefaultConfig() Config {
	return Config{
		WireAPI:             WireAPIChatCompletions,
		ChatCompletionsPath: "/v1/chat/completions",
		ResponsesPath:       "/v1/responses",
		ModelsPath:          "/v1/models",
		AuthHeaderMode:      AuthHeaderAuthorizationBearer,
	}
}

// ParseConfig 严格解析 Adapter 配置，拒绝未知字段与任何秘密字段。
func ParseConfig(raw json.RawMessage) (Config, error) {
	config := DefaultConfig()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("{}")) {
		return config, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, ErrInvalidConfig
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return Config{}, ErrInvalidConfig
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateConfig(config Config) error {
	if config.WireAPI != WireAPIChatCompletions && config.WireAPI != WireAPIResponses {
		return ErrInvalidConfig
	}
	if config.AuthHeaderMode != AuthHeaderAuthorizationBearer && config.AuthHeaderMode != AuthHeaderXAPIKey {
		return ErrInvalidConfig
	}
	for _, path := range []string{config.ChatCompletionsPath, config.ResponsesPath, config.ModelsPath} {
		if !validRelativePath(path) {
			return ErrInvalidConfig
		}
	}
	return nil
}

func validRelativePath(value string) bool {
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return false
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func Schema() json.RawMessage {
	return json.RawMessage(`{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "wire_api":{"enum":["chat_completions","responses"],"default":"chat_completions"},
    "chat_completions_path":{"type":"string","default":"/v1/chat/completions"},
    "responses_path":{"type":"string","default":"/v1/responses"},
    "models_path":{"type":"string","default":"/v1/models"},
    "auth_header_mode":{"enum":["authorization_bearer","x_api_key"],"default":"authorization_bearer"}
  }
}`)
}

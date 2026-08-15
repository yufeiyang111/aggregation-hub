package bootstrap

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"aggregationhub.local/core/internal/config"
)

const (
	MinManagementTokenBytes = 32
	MaxManagementTokenBytes = 256
	MaxBootstrapLineBytes   = 4096
	MaxDataDirBytes         = 2048
)

var (
	errInvalidBootstrapInput = errors.New("bootstrap 输入无效")
	errSecretsMarshalBlocked = errors.New("bootstrap secrets 禁止序列化")
)

// BootstrapSecrets 只允许从受限 stdin 解码，不允许被 JSON 序列化到日志或响应。
type BootstrapSecrets struct {
	ManagementToken string `json:"management_token"`
	DataDir         string `json:"data_dir"`
}

// MarshalJSON 阻止秘密对象被意外写入 JSON、日志或测试快照。
func (BootstrapSecrets) MarshalJSON() ([]byte, error) {
	return nil, errSecretsMarshalBlocked
}

// Clear 释放当前对象持有的令牌引用；Go 字符串本身不承诺原地清零。
func (s *BootstrapSecrets) Clear() {
	if s != nil {
		s.ManagementToken = ""
	}
}

// ReadyEvent 是 Core stdout 唯一允许输出的启动完成事件。
type ReadyEvent struct {
	Event        string `json:"event"`
	ControlURL   string `json:"control_url"`
	DataPlaneURL string `json:"data_plane_url"`
	PID          int    `json:"pid"`
}

type bootstrapSecretsWire struct {
	ManagementToken string `json:"management_token"`
	DataDir         string `json:"data_dir"`
}

// ReadBootstrapSecrets 从 stdin 读取一个有界 JSON 行，并拒绝未知字段和尾随内容。
func ReadBootstrapSecrets(reader io.Reader) (BootstrapSecrets, error) {
	if reader == nil {
		return BootstrapSecrets{}, errInvalidBootstrapInput
	}

	limited := io.LimitReader(reader, MaxBootstrapLineBytes+1)
	buffered := bufio.NewReaderSize(limited, MaxBootstrapLineBytes+1)
	line, err := buffered.ReadBytes('\n')
	if err != nil || len(line) == 0 || len(line) > MaxBootstrapLineBytes {
		return BootstrapSecrets{}, errInvalidBootstrapInput
	}

	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		return BootstrapSecrets{}, errInvalidBootstrapInput
	}

	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()

	var wire bootstrapSecretsWire
	if err := decoder.Decode(&wire); err != nil {
		return BootstrapSecrets{}, errInvalidBootstrapInput
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return BootstrapSecrets{}, errInvalidBootstrapInput
	}

	tokenBytes := len(wire.ManagementToken)
	if tokenBytes < MinManagementTokenBytes || tokenBytes > MaxManagementTokenBytes || !validDataDir(wire.DataDir) {
		return BootstrapSecrets{}, errInvalidBootstrapInput
	}

	return BootstrapSecrets{ManagementToken: wire.ManagementToken, DataDir: filepath.Clean(wire.DataDir)}, nil
}

func validDataDir(value string) bool {
	if len(value) == 0 || len(value) > MaxDataDirBytes || strings.TrimSpace(value) != value || !filepath.IsAbs(value) {
		return false
	}
	clean := filepath.Clean(value)
	volume := filepath.VolumeName(clean)
	return clean != "." && clean != volume+string(os.PathSeparator)
}

// WriteReadyEvent 校验并写出恰好一行 JSON；调用方不得再向 stdout 写入其他内容。
func WriteReadyEvent(writer io.Writer, event ReadyEvent) error {
	if writer == nil || !event.valid() {
		return errors.New("ready 事件无效")
	}
	return json.NewEncoder(writer).Encode(event)
}

func (event ReadyEvent) valid() bool {
	return event.Event == "ready" &&
		event.PID > 0 &&
		validLoopbackHTTPURL(event.ControlURL) &&
		validLoopbackHTTPURL(event.DataPlaneURL)
}

func validLoopbackHTTPURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil {
		return false
	}
	if parsed.Hostname() != config.LoopbackHost || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}

	portText := parsed.Port()
	if portText == "" {
		return false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return false
	}

	host, portFromHost, err := net.SplitHostPort(parsed.Host)
	return err == nil && host == config.LoopbackHost && portFromHost == fmt.Sprintf("%d", port)
}

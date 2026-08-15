package transport

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

var ErrSSEEventTooLarge = errors.New("SSE 事件超过大小限制")

type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// SSEReader 以有界缓冲解析 SSE，天然支持任意网络分块和多行 data 字段。
type SSEReader struct {
	reader       *bufio.Reader
	maxEventSize int
	finished     bool
}

func NewSSEReader(source io.Reader, maxEventSize int) *SSEReader {
	if maxEventSize < 1 {
		maxEventSize = 64 * 1024
	}
	return &SSEReader{reader: bufio.NewReader(source), maxEventSize: maxEventSize}
}

func (value *SSEReader) Next() (SSEEvent, error) {
	if value == nil || value.finished {
		return SSEEvent{}, io.EOF
	}
	var event SSEEvent
	var data []string
	seen := false
	size := 0
	for {
		line, err := value.readLine()
		if err != nil && !errors.Is(err, io.EOF) {
			return SSEEvent{}, err
		}
		if line == "" {
			if seen {
				event.Data = strings.Join(data, "\n")
				return event, nil
			}
			if errors.Is(err, io.EOF) {
				value.finished = true
				return SSEEvent{}, io.EOF
			}
			continue
		}
		size += len(line)
		if size > value.maxEventSize {
			return SSEEvent{}, ErrSSEEventTooLarge
		}
		if strings.HasPrefix(line, ":") {
			if errors.Is(err, io.EOF) {
				value.finished = true
				return SSEEvent{}, io.EOF
			}
			continue
		}
		seen = true
		field, fieldValue, found := strings.Cut(line, ":")
		if !found {
			field, fieldValue = line, ""
		} else {
			fieldValue = strings.TrimPrefix(fieldValue, " ")
		}
		switch field {
		case "event":
			event.Event = fieldValue
		case "data":
			data = append(data, fieldValue)
		case "id":
			event.ID = fieldValue
		}
		if errors.Is(err, io.EOF) {
			value.finished = true
			event.Data = strings.Join(data, "\n")
			return event, nil
		}
	}
}

func (value *SSEReader) readLine() (string, error) {
	var parts []byte
	for {
		fragment, prefix, err := value.reader.ReadLine()
		parts = append(parts, fragment...)
		if len(parts) > value.maxEventSize {
			return "", ErrSSEEventTooLarge
		}
		if err != nil {
			return string(parts), err
		}
		if !prefix {
			return string(parts), nil
		}
	}
}

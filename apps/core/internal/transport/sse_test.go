package transport_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/transport"
)

func TestSSEReaderHandlesChunkedMultilineAndComments(t *testing.T) {
	reader := &chunkReader{chunks: []string{"event: message\nda", "ta: first\ndata: second\n", "\n: ignored\ndata: final\n\n"}}
	stream := transport.NewSSEReader(reader, 1024)
	first, err := stream.Next()
	if err != nil || first.Event != "message" || first.Data != "first\nsecond" {
		t.Fatalf("第一个 SSE 事件错误: %+v err=%v", first, err)
	}
	second, err := stream.Next()
	if err != nil || second.Data != "final" {
		t.Fatalf("第二个 SSE 事件错误: %+v err=%v", second, err)
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("流结束错误=%v", err)
	}
}

func TestSSEReaderRejectsOversizedUnfinishedEvent(t *testing.T) {
	stream := transport.NewSSEReader(strings.NewReader("data: 123456789\n\n"), 8)
	if _, err := stream.Next(); !errors.Is(err, transport.ErrSSEEventTooLarge) {
		t.Fatalf("超大 SSE 事件未被拒绝: %v", err)
	}
}

type chunkReader struct {
	chunks []string
	index  int
}

func (reader *chunkReader) Read(buffer []byte) (int, error) {
	if reader.index >= len(reader.chunks) {
		return 0, io.EOF
	}
	value := reader.chunks[reader.index]
	reader.index++
	return copy(buffer, value), nil
}

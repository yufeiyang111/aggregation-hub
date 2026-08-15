package bootstrap_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"aggregationhub.local/core/internal/bootstrap"
)

const (
	testDataDirJSON = `C:\\Users\\test\\AppData\\Local\\AggregationHub`
	testDataDir     = `C:\Users\test\AppData\Local\AggregationHub`
)

func bootstrapLine(token string) string {
	return `{"management_token":"` + token + `","data_dir":"` + testDataDirJSON + `"}` + "\n"
}

func TestReadyEventJSONDoesNotContainManagementToken(t *testing.T) {
	event := bootstrap.ReadyEvent{
		Event:        "ready",
		ControlURL:   "http://127.0.0.1:49152",
		DataPlaneURL: "http://127.0.0.1:18443",
		PID:          42,
	}

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	lower := bytes.ToLower(raw)
	for _, forbidden := range [][]byte{[]byte("management_token"), []byte("token"), []byte("secret")} {
		if bytes.Contains(lower, forbidden) {
			t.Fatalf("ready 事件包含禁止字段 %q: %s", forbidden, raw)
		}
	}
}

func TestBootstrapSecretsCannotBeMarshaled(t *testing.T) {
	secret := bootstrap.BootstrapSecrets{ManagementToken: strings.Repeat("x", 32), DataDir: testDataDir}

	raw, err := json.Marshal(secret)
	if err == nil {
		t.Fatalf("BootstrapSecrets 不得被 JSON 序列化，实际输出: %s", raw)
	}
	if bytes.Contains(raw, []byte(secret.ManagementToken)) || strings.Contains(err.Error(), secret.ManagementToken) {
		t.Fatal("序列化失败信息泄露了管理令牌")
	}
}

func TestReadBootstrapSecretsAcceptsSingleValidLine(t *testing.T) {
	token := strings.Repeat("a", 64)
	secrets, err := bootstrap.ReadBootstrapSecrets(strings.NewReader(bootstrapLine(token)))
	if err != nil {
		t.Fatalf("读取合法 bootstrap 输入失败: %v", err)
	}
	if secrets.ManagementToken != token || secrets.DataDir != testDataDir {
		t.Fatalf("bootstrap 值不匹配: %+v", secrets)
	}
}

func TestReadBootstrapSecretsValidatesInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "缺少换行", input: strings.TrimSuffix(bootstrapLine(strings.Repeat("a", 64)), "\n")},
		{name: "缺少字段", input: `{}` + "\n"},
		{name: "令牌不足三十二字节", input: bootstrapLine(strings.Repeat("a", 31))},
		{name: "令牌超过上限", input: bootstrapLine(strings.Repeat("a", 257))},
		{name: "未知字段", input: `{"management_token":"` + strings.Repeat("a", 64) + `","data_dir":"` + testDataDirJSON + `","unexpected":true}` + "\n"},
		{name: "同一行尾随数据", input: `{"management_token":"` + strings.Repeat("a", 64) + `","data_dir":"` + testDataDirJSON + `"} trailing` + "\n"},
		{name: "字段类型错误", input: `{"management_token":42,"data_dir":42}` + "\n"},
		{name: "相对数据目录", input: `{"management_token":"` + strings.Repeat("a", 64) + `","data_dir":"relative"}` + "\n"},
		{name: "卷根目录", input: `{"management_token":"` + strings.Repeat("a", 64) + `","data_dir":"C:\\"}` + "\n"},
		{name: "超过单行输入上限", input: strings.Repeat("x", bootstrap.MaxBootstrapLineBytes+1) + "\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets, err := bootstrap.ReadBootstrapSecrets(strings.NewReader(test.input))
			if err == nil {
				t.Fatalf("非法输入被接受，令牌长度: %d", len(secrets.ManagementToken))
			}
			if strings.Contains(err.Error(), strings.Repeat("a", 31)) {
				t.Fatal("校验错误泄露了输入令牌")
			}
		})
	}
}

func TestReadBootstrapSecretsMeasuresTokenInBytes(t *testing.T) {
	// 11 个汉字是 33 个 UTF-8 字节，满足“至少 32 bytes”的协议要求。
	token := strings.Repeat("密", 11)
	secrets, err := bootstrap.ReadBootstrapSecrets(strings.NewReader(bootstrapLine(token)))
	if err != nil {
		t.Fatalf("按字节计数的合法令牌被拒绝: %v", err)
	}
	if len(secrets.ManagementToken) != 33 {
		t.Fatalf("令牌字节数错误: %d", len(secrets.ManagementToken))
	}
}

func TestWriteReadyEventWritesExactlyOneJSONLine(t *testing.T) {
	var output bytes.Buffer
	event := bootstrap.ReadyEvent{
		Event:        "ready",
		ControlURL:   "http://127.0.0.1:49152",
		DataPlaneURL: "http://127.0.0.1:18443",
		PID:          42,
	}

	if err := bootstrap.WriteReadyEvent(&output, event); err != nil {
		t.Fatalf("写 ready 事件失败: %v", err)
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 || !bytes.HasSuffix(output.Bytes(), []byte("\n")) {
		t.Fatalf("ready 事件必须恰好占一行: %q", output.Bytes())
	}

	var decoded bootstrap.ReadyEvent
	if err := json.Unmarshal(bytes.TrimSuffix(output.Bytes(), []byte("\n")), &decoded); err != nil {
		t.Fatalf("ready 事件不是合法 JSON: %v", err)
	}
	if decoded != event {
		t.Fatalf("ready 事件往返不一致: %#v", decoded)
	}
}

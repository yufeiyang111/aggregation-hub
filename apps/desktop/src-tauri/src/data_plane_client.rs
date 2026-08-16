use std::io::{Read, Write};
use std::net::{SocketAddr, TcpStream};
use std::time::Duration;

use serde::Serialize;
use serde_json::{json, Value};

const DATA_PLANE_TIMEOUT: Duration = Duration::from_secs(15);
const MAX_DATA_PLANE_RESPONSE_BYTES: u64 = 256 * 1024;

#[derive(Clone, Debug, Serialize)]
pub struct LocalResponsesTestResult {
    pub success: bool,
    pub kind: String,
    pub code: String,
    pub message: String,
    pub http_status: u16,
}

pub(crate) fn test_responses(
    data_plane_url: &str,
    local_key: &mut [u8],
    model: &str,
    kind: &str,
) -> Result<LocalResponsesTestResult, String> {
    let result = (|| {
        let address = parse_loopback_data_plane_url(data_plane_url)?;
        if local_key.is_empty() || local_key.len() > 512 || !valid_model(model) {
            return Err("本地 Responses 测试参数无效".to_owned());
        }
        let payload = build_payload(model, kind)?;
        let mut body =
            serde_json::to_vec(&payload).map_err(|_| "本地测试请求编码失败".to_owned())?;
        let response = execute(address, local_key, &body, kind);
        body.fill(0);
        response
    })();
    local_key.fill(0);
    result
}

fn build_payload(model: &str, kind: &str) -> Result<Value, String> {
    match kind {
        "text" => Ok(json!({
            "model": model,
            "input": "Reply with exactly OK.",
            "max_output_tokens": 16
        })),
        "function" => Ok(json!({
            "model": model,
            "input": "Call the ping function with value ok.",
            "max_output_tokens": 32,
            "tools": [{
                "type": "function",
                "name": "ping",
                "description": "Returns a small diagnostic value.",
                "parameters": {
                    "type": "object",
                    "properties": {"value": {"type": "string"}},
                    "required": ["value"],
                    "additionalProperties": false
                }
            }],
            "tool_choice": {"type": "function", "name": "ping"}
        })),
        _ => Err("本地 Responses 测试类型无效".to_owned()),
    }
}

fn execute(
    address: SocketAddr,
    local_key: &[u8],
    body: &[u8],
    kind: &str,
) -> Result<LocalResponsesTestResult, String> {
    let key =
        std::str::from_utf8(local_key).map_err(|_| "本地 Responses 测试参数无效".to_owned())?;
    let mut stream = TcpStream::connect_timeout(&address, DATA_PLANE_TIMEOUT)
        .map_err(|_| "无法连接本地网关".to_owned())?;
    stream
        .set_read_timeout(Some(DATA_PLANE_TIMEOUT))
        .map_err(|_| "设置本地网关读取超时失败".to_owned())?;
    stream
        .set_write_timeout(Some(DATA_PLANE_TIMEOUT))
        .map_err(|_| "设置本地网关写入超时失败".to_owned())?;

    let mut request = format!(
        "POST /v1/responses HTTP/1.0\r\nHost: 127.0.0.1\r\nAuthorization: Bearer {key}\r\nContent-Type: application/json\r\nAccept: application/json\r\nConnection: close\r\nContent-Length: {}\r\n\r\n",
        body.len()
    )
    .into_bytes();
    request.extend_from_slice(body);
    let write_result = stream.write_all(&request);
    request.fill(0);
    write_result.map_err(|_| "发送本地网关测试请求失败".to_owned())?;

    let mut response = Vec::new();
    stream
        .take(MAX_DATA_PLANE_RESPONSE_BYTES)
        .read_to_end(&mut response)
        .map_err(|_| "读取本地网关测试响应失败".to_owned())?;
    let result = classify_response(&response, kind);
    response.fill(0);
    result
}

fn parse_loopback_data_plane_url(value: &str) -> Result<SocketAddr, String> {
    let trimmed = value.trim();
    let without_scheme = trimmed
        .strip_prefix("http://")
        .ok_or_else(|| "本地网关地址无效".to_owned())?;
    if without_scheme.contains('/') || without_scheme.contains('?') || without_scheme.contains('#')
    {
        return Err("本地网关地址无效".to_owned());
    }
    let address: SocketAddr = without_scheme
        .parse()
        .map_err(|_| "本地网关地址无效".to_owned())?;
    if !address.ip().is_loopback() || address.port() == 0 {
        return Err("本地网关地址无效".to_owned());
    }
    Ok(address)
}

fn valid_model(value: &str) -> bool {
    !value.is_empty() && value.len() <= 304 && !value.chars().any(char::is_whitespace)
}

fn classify_response(response: &[u8], kind: &str) -> Result<LocalResponsesTestResult, String> {
    let Some(separator) = response.windows(4).position(|window| window == b"\r\n\r\n") else {
        return Err("本地网关测试响应无效".to_owned());
    };
    let header = std::str::from_utf8(&response[..separator])
        .map_err(|_| "本地网关测试响应无效".to_owned())?;
    let status = header
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|value| value.parse::<u16>().ok())
        .ok_or_else(|| "本地网关测试响应无效".to_owned())?;
    if !(200..300).contains(&status) {
        return Ok(LocalResponsesTestResult {
            success: false,
            kind: kind.to_owned(),
            code: match status {
                401 => "local_key_rejected",
                404 => "responses_not_available",
                429 => "gateway_rate_limited",
                _ => "gateway_request_failed",
            }
            .to_owned(),
            message: "本地网关未通过该诊断请求".to_owned(),
            http_status: status,
        });
    }
    let payload: Value = serde_json::from_slice(&response[separator + 4..])
        .map_err(|_| "本地网关测试响应无效".to_owned())?;
    let valid = match kind {
        "text" => payload
            .get("output")
            .and_then(Value::as_array)
            .is_some_and(|items| items.iter().any(has_output_text)),
        "function" => payload
            .get("output")
            .and_then(Value::as_array)
            .is_some_and(|items| items.iter().any(has_function_call)),
        _ => false,
    };
    Ok(LocalResponsesTestResult {
        success: valid,
        kind: kind.to_owned(),
        code: if valid {
            "ok"
        } else {
            "expected_output_missing"
        }
        .to_owned(),
        message: if valid {
            "本地 Responses 测试通过"
        } else {
            "网关响应未包含预期输出"
        }
        .to_owned(),
        http_status: status,
    })
}

fn has_output_text(item: &Value) -> bool {
    item.get("type") == Some(&Value::String("message".to_owned()))
        && item
            .get("content")
            .and_then(Value::as_array)
            .is_some_and(|content| {
                content.iter().any(|part| {
                    part.get("type") == Some(&Value::String("output_text".to_owned()))
                        && part
                            .get("text")
                            .and_then(Value::as_str)
                            .is_some_and(|text| !text.is_empty())
                })
            })
}

fn has_function_call(item: &Value) -> bool {
    item.get("type") == Some(&Value::String("function_call".to_owned()))
        && item
            .get("name")
            .and_then(Value::as_str)
            .is_some_and(|name| !name.is_empty())
}

#[cfg(test)]
mod tests {
    use super::{classify_response, parse_loopback_data_plane_url};

    #[test]
    fn accepts_only_loopback_data_plane_addresses() {
        assert_eq!(
            parse_loopback_data_plane_url("http://127.0.0.1:18443")
                .unwrap()
                .port(),
            18443
        );
        assert!(parse_loopback_data_plane_url("https://127.0.0.1:18443").is_err());
        assert!(parse_loopback_data_plane_url("http://localhost:18443").is_err());
        assert!(parse_loopback_data_plane_url("http://10.0.0.1:18443").is_err());
    }

    #[test]
    fn clears_local_key_after_early_validation_failure() {
        let mut key = b"ah_local_secret".to_vec();
        assert!(super::test_responses("invalid", &mut key, "bundle/model", "invalid").is_err());
        assert!(key.iter().all(|value| *value == 0));
    }

    #[test]
    fn validates_only_expected_responses_output() {
        let text = classify_response(b"HTTP/1.0 200 OK\r\n\r\n{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}]}", "text").unwrap();
        assert!(text.success);
        let function = classify_response(
            b"HTTP/1.0 200 OK\r\n\r\n{\"output\":[{\"type\":\"function_call\",\"name\":\"ping\"}]}",
            "function",
        )
        .unwrap();
        assert!(function.success);
        let rejected = classify_response(b"HTTP/1.0 401 Unauthorized\r\n\r\n{}", "text").unwrap();
        assert!(!rejected.success);
        assert_eq!(rejected.code, "local_key_rejected");
    }
}

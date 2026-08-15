use std::io::{Read, Write};
use std::net::{SocketAddr, TcpStream};
use std::time::Duration;

use serde::de::DeserializeOwned;
use serde::Serialize;

const CONTROL_REQUEST_TIMEOUT: Duration = Duration::from_secs(3);
const MAX_CONTROL_RESPONSE_BYTES: u64 = 256 * 1024;
const MANAGEMENT_TOKEN_HEADER: &str = "X-Aggregation-Hub-Management-Token";

pub(crate) fn get_json<T: DeserializeOwned>(
    control_url: &str,
    token: &str,
    path: &str,
) -> Result<T, String> {
    execute_json(control_url, token, "GET", path, None)
}

pub(crate) fn post_json<Request: Serialize, Response: DeserializeOwned>(
    control_url: &str,
    token: &str,
    path: &str,
    payload: &Request,
) -> Result<Response, String> {
    let mut body = serde_json::to_vec(payload).map_err(|_| "管理请求编码失败".to_owned())?;
    let result = execute_json(control_url, token, "POST", path, Some(&body));
    body.fill(0);
    result
}

pub(crate) fn delete_json<Request: Serialize>(
    control_url: &str,
    token: &str,
    path: &str,
    payload: &Request,
) -> Result<(), String> {
    let mut body = serde_json::to_vec(payload).map_err(|_| "管理请求编码失败".to_owned())?;
    let result = execute_empty(control_url, token, "DELETE", path, Some(&body));
    body.fill(0);
    result
}

fn execute_json<T: DeserializeOwned>(
    control_url: &str,
    token: &str,
    method: &str,
    path: &str,
    body: Option<&[u8]>,
) -> Result<T, String> {
    let address = parse_loopback_control_url(control_url)?;
    if !valid_path(path) || token.len() < 32 {
        return Err("Core 管理连接不可用".to_owned());
    }

    let mut stream = TcpStream::connect_timeout(&address, CONTROL_REQUEST_TIMEOUT)
        .map_err(|_| "无法连接本地 Core 管理接口".to_owned())?;
    stream
        .set_read_timeout(Some(CONTROL_REQUEST_TIMEOUT))
        .map_err(|_| "设置本地 Core 读取超时失败".to_owned())?;
    stream
        .set_write_timeout(Some(CONTROL_REQUEST_TIMEOUT))
        .map_err(|_| "设置本地 Core 写入超时失败".to_owned())?;

    let content_length = body.map_or(0, <[u8]>::len);
    let mut request = format!(
        "{method} {path} HTTP/1.0\r\nHost: 127.0.0.1\r\n{MANAGEMENT_TOKEN_HEADER}: {token}\r\nAccept: application/json\r\nConnection: close\r\nContent-Length: {content_length}\r\n"
    )
    .into_bytes();
    if body.is_some() {
        request.extend_from_slice(b"Content-Type: application/json\r\n");
    }
    request.extend_from_slice(b"\r\n");
    if let Some(body) = body {
        request.extend_from_slice(body);
    }

    let write_result = stream.write_all(&request).and_then(|_| stream.flush());
    request.fill(0);
    write_result.map_err(|_| "本地 Core 管理请求失败".to_owned())?;

    let mut response = Vec::new();
    let read_result = stream
        .take(MAX_CONTROL_RESPONSE_BYTES + 1)
        .read_to_end(&mut response);
    if read_result.is_err() || response.len() as u64 > MAX_CONTROL_RESPONSE_BYTES {
        return Err("本地 Core 管理响应无效".to_owned());
    }
    decode_success_response(&response)
}

fn execute_empty(
    control_url: &str,
    token: &str,
    method: &str,
    path: &str,
    body: Option<&[u8]>,
) -> Result<(), String> {
    let address = parse_loopback_control_url(control_url)?;
    if !valid_path(path) || token.len() < 32 {
        return Err("Core 管理连接不可用".to_owned());
    }
    let mut stream = TcpStream::connect_timeout(&address, CONTROL_REQUEST_TIMEOUT)
        .map_err(|_| "无法连接本地 Core 管理接口".to_owned())?;
    stream
        .set_read_timeout(Some(CONTROL_REQUEST_TIMEOUT))
        .map_err(|_| "设置本地 Core 读取超时失败".to_owned())?;
    stream
        .set_write_timeout(Some(CONTROL_REQUEST_TIMEOUT))
        .map_err(|_| "设置本地 Core 写入超时失败".to_owned())?;
    let content_length = body.map_or(0, <[u8]>::len);
    let mut request = format!("{method} {path} HTTP/1.0\r\nHost: 127.0.0.1\r\n{MANAGEMENT_TOKEN_HEADER}: {token}\r\nAccept: application/json\r\nConnection: close\r\nContent-Length: {content_length}\r\n").into_bytes();
    if body.is_some() {
        request.extend_from_slice(b"Content-Type: application/json\r\n");
    }
    request.extend_from_slice(b"\r\n");
    if let Some(body) = body {
        request.extend_from_slice(body);
    }
    let write_result = stream.write_all(&request).and_then(|_| stream.flush());
    request.fill(0);
    write_result.map_err(|_| "本地 Core 管理请求失败".to_owned())?;
    let mut response = Vec::new();
    let read_result = stream
        .take(MAX_CONTROL_RESPONSE_BYTES + 1)
        .read_to_end(&mut response);
    if read_result.is_err() || response.len() as u64 > MAX_CONTROL_RESPONSE_BYTES {
        return Err("本地 Core 管理响应无效".to_owned());
    }
    let Some(separator) = response.windows(4).position(|window| window == b"\r\n\r\n") else {
        return Err("本地 Core 管理响应无效".to_owned());
    };
    let header = std::str::from_utf8(&response[..separator])
        .map_err(|_| "本地 Core 管理响应无效".to_owned())?;
    let status = header
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|value| value.parse::<u16>().ok())
        .ok_or_else(|| "本地 Core 管理响应无效".to_owned())?;
    if status != 204 {
        return Err("Core 管理请求未能完成".to_owned());
    }
    Ok(())
}

fn parse_loopback_control_url(value: &str) -> Result<SocketAddr, String> {
    let Some(port) = value.strip_prefix("http://127.0.0.1:") else {
        return Err("Core 管理地址无效".to_owned());
    };
    if port.is_empty() || !port.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err("Core 管理地址无效".to_owned());
    }
    let port = port
        .parse::<u16>()
        .ok()
        .filter(|port| *port > 0)
        .ok_or_else(|| "Core 管理地址无效".to_owned())?;
    Ok(SocketAddr::from(([127, 0, 0, 1], port)))
}

fn valid_path(path: &str) -> bool {
    let (base, query) = match path.split_once('?') {
        Some((base, query)) => (base, Some(query)),
        None => (path, None),
    };
    if !base.starts_with("/internal/v1/")
        || base.len() > 256
        || !base
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'/' | b'-' | b'_'))
    {
        return false;
    }
    match query {
        None => true,
        Some(value) => {
            !value.is_empty()
                && value.len() <= 512
                && value.bytes().all(|byte| {
                    byte.is_ascii_alphanumeric()
                        || matches!(byte, b'=' | b'&' | b'%' | b'-' | b'_' | b'.' | b'~')
                })
        }
    }
}

fn decode_success_response<T: DeserializeOwned>(response: &[u8]) -> Result<T, String> {
    let Some(separator) = response.windows(4).position(|window| window == b"\r\n\r\n") else {
        return Err("本地 Core 管理响应无效".to_owned());
    };
    let header = std::str::from_utf8(&response[..separator])
        .map_err(|_| "本地 Core 管理响应无效".to_owned())?;
    let status = header
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|value| value.parse::<u16>().ok())
        .ok_or_else(|| "本地 Core 管理响应无效".to_owned())?;
    if !(200..300).contains(&status) {
        return Err("Core 管理请求未能完成".to_owned());
    }
    serde_json::from_slice(&response[separator + 4..])
        .map_err(|_| "本地 Core 管理响应无效".to_owned())
}

#[cfg(test)]
mod tests {
    use super::{decode_success_response, parse_loopback_control_url, valid_path};
    use serde::Deserialize;

    #[derive(Deserialize)]
    struct Value {
        value: String,
    }

    #[test]
    fn accepts_only_loopback_control_addresses() {
        assert_eq!(
            parse_loopback_control_url("http://127.0.0.1:49152")
                .unwrap()
                .port(),
            49152
        );
        assert!(parse_loopback_control_url("https://127.0.0.1:49152").is_err());
        assert!(parse_loopback_control_url("http://localhost:49152").is_err());
        assert!(parse_loopback_control_url("http://127.0.0.1:0").is_err());
    }

    #[test]
    fn accepts_only_internal_paths() {
        assert!(valid_path("/internal/v1/providers"));
        assert!(!valid_path("/v1/models"));
        assert!(valid_path(
            "/internal/v1/models?page_size=50&search=tool%20name"
        ));
        assert!(!valid_path("/internal/v1/providers?cursor=unsafe space"));
        assert!(!valid_path("/internal/v1/providers?cursor=<script>"));
    }

    #[test]
    fn parses_only_successful_json_response() {
        let value: Value = decode_success_response(
            b"HTTP/1.0 201 Created\r\nContent-Type: application/json\r\n\r\n{\"value\":\"ok\"}",
        )
        .unwrap();
        assert_eq!(value.value, "ok");
        assert!(decode_success_response::<Value>(b"HTTP/1.0 401 Unauthorized\r\n\r\n{}").is_err());
        assert!(decode_success_response::<Value>(b"HTTP/1.0 200 OK\r\n\r\nnot-json").is_err());
    }
}

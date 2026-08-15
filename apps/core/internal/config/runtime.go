package config

// LoopbackHost 是 Data Plane 唯一允许绑定的回环地址。
const LoopbackHost = "127.0.0.1"

type Runtime struct {
	Version    string
	ListenPort int
}

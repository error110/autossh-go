package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 主配置结构
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Tunnels   TunnelsConfig   `mapstructure:"tunnels"`
	Reconnect ReconnectConfig `mapstructure:"reconnect"`
	LogLevel  string          `mapstructure:"log_level"`
}

// ServerConfig SSH服务器配置
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	User string `mapstructure:"user"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	Type       string `mapstructure:"type"` // "password" 或 "key"
	Password   string `mapstructure:"password"`
	KeyFile    string `mapstructure:"key_file"`
	Passphrase string `mapstructure:"passphrase"` // 密钥密码短语
}

// TunnelsConfig 隧道配置
type TunnelsConfig struct {
	Local   []LocalTunnel   `mapstructure:"local"`
	Remote  []RemoteTunnel  `mapstructure:"remote"`
	Dynamic []DynamicTunnel `mapstructure:"dynamic"`
}

// LocalTunnel 本地端口转发配置 (-L)
type LocalTunnel struct {
	Bind   string `mapstructure:"bind"`   // 本地监听地址 (例如: 127.0.0.1:8080)
	Target string `mapstructure:"target"` // 远程目标地址 (例如: localhost:80)
}

// RemoteTunnel 远程端口转发配置 (-R)
type RemoteTunnel struct {
	Bind   string `mapstructure:"bind"`   // 远程监听地址 (例如: 0.0.0.0:9090)
	Target string `mapstructure:"target"` // 本地目标地址 (例如: localhost:22)
}

// DynamicTunnel 动态端口转发配置 (-D)
type DynamicTunnel struct {
	Bind string `mapstructure:"bind"` // 本地SOCKS5监听地址 (例如: 127.0.0.1:1080)
}

// ReconnectConfig 自动重连配置
type ReconnectConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	Interval   time.Duration `mapstructure:"interval"`
	MaxRetries int           `mapstructure:"max_retries"` // 0 = 无限重试
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 22,
		},
		Auth: AuthConfig{
			Type: "key",
		},
		Reconnect: ReconnectConfig{
			Enabled:    true,
			Interval:   5 * time.Second,
			MaxRetries: 0,
		},
		LogLevel: "info",
	}
}

// LoadFromFile 从配置文件加载配置
func LoadFromFile(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	if configPath == "" {
		// 尝试在当前目录和用户目录查找配置文件
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.autossh")
	} else {
		viper.SetConfigFile(configPath)
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// 配置文件不存在，使用默认配置
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 展开密钥文件路径中的 ~
	if cfg.Auth.KeyFile != "" {
		cfg.Auth.KeyFile = expandPath(cfg.Auth.KeyFile)
	}

	return cfg, nil
}

// ParseTarget 解析 user@host:port 格式的目标地址
func ParseTarget(target string) (user, host string, port int, err error) {
	port = 22 // 默认端口

	// 解析 user@host:port
	if idx := strings.Index(target, "@"); idx != -1 {
		user = target[:idx]
		target = target[idx+1:]
	}

	// 解析 host:port
	if idx := strings.LastIndex(target, ":"); idx != -1 {
		host = target[:idx]
		_, err = fmt.Sscanf(target[idx+1:], "%d", &port)
		if err != nil {
			return "", "", 0, fmt.Errorf("无效的端口号: %s", target[idx+1:])
		}
	} else {
		host = target
	}

	if host == "" {
		return "", "", 0, fmt.Errorf("未指定主机")
	}

	return user, host, port, nil
}

// ParseLocalForward 解析本地转发参数 (-L)
// 格式: [bind_address:]port:host:hostport
func ParseLocalForward(spec string) (*LocalTunnel, error) {
	parts := strings.Split(spec, ":")
	switch len(parts) {
	case 3:
		// port:host:hostport
		return &LocalTunnel{
			Bind:   "127.0.0.1:" + parts[0],
			Target: parts[1] + ":" + parts[2],
		}, nil
	case 4:
		// bind_address:port:host:hostport
		return &LocalTunnel{
			Bind:   parts[0] + ":" + parts[1],
			Target: parts[2] + ":" + parts[3],
		}, nil
	default:
		return nil, fmt.Errorf("无效的本地转发格式: %s (期望: [bind_address:]port:host:hostport)", spec)
	}
}

// ParseRemoteForward 解析远程转发参数 (-R)
// 格式: [bind_address:]port:host:hostport
func ParseRemoteForward(spec string) (*RemoteTunnel, error) {
	parts := strings.Split(spec, ":")
	switch len(parts) {
	case 3:
		// port:host:hostport
		return &RemoteTunnel{
			Bind:   "0.0.0.0:" + parts[0],
			Target: parts[1] + ":" + parts[2],
		}, nil
	case 4:
		// bind_address:port:host:hostport
		return &RemoteTunnel{
			Bind:   parts[0] + ":" + parts[1],
			Target: parts[2] + ":" + parts[3],
		}, nil
	default:
		return nil, fmt.Errorf("无效的远程转发格式: %s (期望: [bind_address:]port:host:hostport)", spec)
	}
}

// ParseDynamicForward 解析动态转发参数 (-D)
// 格式: [bind_address:]port
func ParseDynamicForward(spec string) (*DynamicTunnel, error) {
	if strings.Contains(spec, ":") {
		return &DynamicTunnel{Bind: spec}, nil
	}
	// 只有端口号
	return &DynamicTunnel{Bind: "127.0.0.1:" + spec}, nil
}

// expandPath 展开路径中的 ~ 为用户主目录
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// Validate 验证配置的有效性
func (c *Config) Validate() error {
	if c.Server.Host == "" {
		return fmt.Errorf("未指定服务器地址")
	}
	if c.Server.User == "" {
		return fmt.Errorf("未指定用户名")
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("无效的端口号: %d", c.Server.Port)
	}

	switch c.Auth.Type {
	case "password":
		// 密码可以为空，运行时会提示输入
	case "key":
		if c.Auth.KeyFile == "" {
			// 使用默认密钥路径
			home, err := os.UserHomeDir()
			if err == nil {
				c.Auth.KeyFile = filepath.Join(home, ".ssh", "id_rsa")
			}
		}
	default:
		return fmt.Errorf("无效的认证类型: %s (期望: password 或 key)", c.Auth.Type)
	}

	// 检查是否有至少一个隧道配置
	if len(c.Tunnels.Local) == 0 && len(c.Tunnels.Remote) == 0 && len(c.Tunnels.Dynamic) == 0 {
		return fmt.Errorf("未配置任何隧道")
	}

	return nil
}

// Address 返回服务器地址
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}


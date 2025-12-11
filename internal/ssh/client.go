package ssh

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"autossh/internal/config"

	"golang.org/x/crypto/ssh"
)

// Client SSH客户端
type Client struct {
	cfg    *config.Config
	conn   *ssh.Client
	mu     sync.RWMutex
	closed bool
}

// NewClient 创建新的SSH客户端
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
	}
}

// Connect 建立SSH连接
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	// 获取认证方法
	authMethods, err := GetAuthMethods(c.cfg)
	if err != nil {
		return fmt.Errorf("获取认证方法失败: %w", err)
	}

	// SSH 客户端配置
	sshConfig := &ssh.ClientConfig{
		User:            c.cfg.Server.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 生产环境应验证主机密钥
		Timeout:         30 * time.Second,
	}

	// 建立连接
	address := c.cfg.Address()
	slog.Debug("正在连接SSH服务器", "address", address)

	conn, err := ssh.Dial("tcp", address, sshConfig)
	if err != nil {
		return fmt.Errorf("SSH连接失败: %w", err)
	}

	c.conn = conn
	c.closed = false
	slog.Info("SSH连接已建立", "address", address)

	return nil
}

// Close 关闭SSH连接
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// IsClosed 检查连接是否已关闭
func (c *Client) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// IsConnected 检查连接状态
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil || c.closed {
		return false
	}

	// 通过发送请求测试连接
	_, _, err := c.conn.SendRequest("keepalive@autossh", true, nil)
	return err == nil
}

// Dial 通过SSH隧道建立连接
func (c *Client) Dial(network, address string) (net.Conn, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return nil, fmt.Errorf("SSH未连接")
	}

	return c.conn.Dial(network, address)
}

// Listen 在远程服务器上监听端口
func (c *Client) Listen(network, address string) (net.Listener, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return nil, fmt.Errorf("SSH未连接")
	}

	return c.conn.Listen(network, address)
}

// GetConn 获取底层SSH连接（用于高级操作）
func (c *Client) GetConn() *ssh.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// KeepAlive 发送保活请求
func (c *Client) KeepAlive() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.conn == nil {
		return fmt.Errorf("SSH未连接")
	}

	_, _, err := c.conn.SendRequest("keepalive@autossh", true, nil)
	return err
}

// StartKeepAlive 启动保活goroutine
func (c *Client) StartKeepAlive(interval time.Duration, errChan chan<- error) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			<-ticker.C
			if c.IsClosed() {
				return
			}

			if err := c.KeepAlive(); err != nil {
				slog.Warn("保活请求失败", "error", err)
				errChan <- err
				return
			}
			slog.Debug("保活请求成功")
		}
	}()
}

// Config 返回配置
func (c *Client) Config() *config.Config {
	return c.cfg
}


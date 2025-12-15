package tunnel

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"autossh/internal/config"
	"autossh/internal/ssh"
)

// Manager 隧道管理器
type Manager struct {
	client  *ssh.Client
	cfg     *config.Config
	tunnels []Tunnel
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

// Tunnel 隧道接口
type Tunnel interface {
	Start(ctx context.Context) error
	Stop() error
	Type() string
	String() string
}

// NewManager 创建隧道管理器
func NewManager(client *ssh.Client, cfg *config.Config) *Manager {
	return &Manager{
		client: client,
		cfg:    cfg,
	}
}

// Start 启动所有隧道
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.tunnels = nil

	// 创建本地转发隧道
	for _, spec := range m.cfg.Tunnels.Local {
		tunnel := NewLocalTunnel(m.client, spec)
		m.tunnels = append(m.tunnels, tunnel)
	}

	// 创建远程转发隧道
	for _, spec := range m.cfg.Tunnels.Remote {
		tunnel := NewRemoteTunnel(m.client, spec)
		m.tunnels = append(m.tunnels, tunnel)
	}

	// 创建动态转发隧道
	for _, spec := range m.cfg.Tunnels.Dynamic {
		tunnel := NewDynamicTunnel(m.client, spec)
		m.tunnels = append(m.tunnels, tunnel)
	}

	if len(m.tunnels) == 0 {
		return nil
	}

	// 启动所有隧道，收集初始化错误
	errChan := make(chan error, len(m.tunnels))

	for _, t := range m.tunnels {
		slog.Info("启动隧道", "type", t.Type(), "spec", t.String())
		go func(tunnel Tunnel) {
			if err := tunnel.Start(m.ctx); err != nil {
				// 只有在 context 未取消时才记录错误
				select {
				case <-m.ctx.Done():
					// context 已取消，这是正常停止
				default:
					slog.Error("隧道启动失败", "type", tunnel.Type(), "spec", tunnel.String(), "error", err)
					select {
					case errChan <- err:
					default:
						// channel 已满，忽略
					}
				}
			}
		}(t)
	}

	// 等待一小段时间以捕获初始化错误（如监听失败）
	// 如果隧道成功启动，Start() 会阻塞在 Accept() 循环中
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	select {
	case err := <-errChan:
		// 有隧道启动失败
		if m.cancel != nil {
			m.cancel()
		}
		return err
	case <-timeoutCtx.Done():
		// 100ms 内没有错误，认为启动成功
		return nil
	}
}

// Stop 停止所有隧道
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	for _, t := range m.tunnels {
		slog.Debug("停止隧道", "type", t.Type(), "spec", t.String())
		if err := t.Stop(); err != nil {
			slog.Warn("停止隧道失败", "type", t.Type(), "error", err)
		}
	}

	m.tunnels = nil
}

// Restart 重启所有隧道
func (m *Manager) Restart() error {
	m.Stop()
	return m.Start()
}

// TunnelCount 返回隧道数量
func (m *Manager) TunnelCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.tunnels)
}

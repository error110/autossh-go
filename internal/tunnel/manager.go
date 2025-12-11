package tunnel

import (
	"context"
	"log/slog"
	"sync"

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

	// 启动所有隧道
	for _, t := range m.tunnels {
		slog.Info("启动隧道", "type", t.Type(), "spec", t.String())
		go func(tunnel Tunnel) {
			if err := tunnel.Start(m.ctx); err != nil {
				slog.Error("隧道启动失败", "type", tunnel.Type(), "spec", tunnel.String(), "error", err)
			}
		}(t)
	}

	return nil
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


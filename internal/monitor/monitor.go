package monitor

import (
	"log/slog"
	"sync"
	"time"

	"autossh/internal/config"
	"autossh/internal/ssh"
	"autossh/internal/tunnel"
)

// Monitor 连接监控器
type Monitor struct {
	client    *ssh.Client
	tunnelMgr *tunnel.Manager
	cfg       *config.Config
	stopCh    chan struct{}
	mu        sync.Mutex
	running   bool
}

// NewMonitor 创建监控器
func NewMonitor(client *ssh.Client, tunnelMgr *tunnel.Manager, cfg *config.Config) *Monitor {
	return &Monitor{
		client:    client,
		tunnelMgr: tunnelMgr,
		cfg:       cfg,
		stopCh:    make(chan struct{}),
	}
}

// Start 启动监控器
func (m *Monitor) Start() error {
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()

	retryCount := 0
	maxRetries := m.cfg.Reconnect.MaxRetries
	interval := m.cfg.Reconnect.Interval

	for {
		// 检查是否应该停止
		select {
		case <-m.stopCh:
			slog.Info("监控器已停止")
			return nil
		default:
		}

		// 建立连接
		if err := m.client.Connect(); err != nil {
			slog.Error("连接失败", "error", err)

			if !m.cfg.Reconnect.Enabled {
				return err
			}

			retryCount++
			if maxRetries > 0 && retryCount >= maxRetries {
				slog.Error("达到最大重试次数", "count", retryCount)
				return err
			}

			waitTime := m.calculateBackoff(retryCount, interval)
			slog.Info("等待重连", "seconds", waitTime.Seconds(), "attempt", retryCount)

			select {
			case <-m.stopCh:
				return nil
			case <-time.After(waitTime):
				continue
			}
		}

		// 连接成功，重置重试计数
		retryCount = 0

		// 启动隧道
		if err := m.tunnelMgr.Start(); err != nil {
			slog.Error("启动隧道失败", "error", err)
			m.client.Close()
			continue
		}

		// 启动保活和监控
		errChan := make(chan error, 1)
		m.client.StartKeepAlive(30*time.Second, errChan)

		// 等待连接断开或停止信号
		select {
		case <-m.stopCh:
			m.tunnelMgr.Stop()
			m.client.Close()
			return nil

		case err := <-errChan:
			slog.Warn("连接断开", "error", err)
			m.tunnelMgr.Stop()
			m.client.Close()

			if !m.cfg.Reconnect.Enabled {
				return err
			}

			slog.Info("准备重连...")
		}
	}
}

// Stop 停止监控器
func (m *Monitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		close(m.stopCh)
		m.running = false
	}
}

// calculateBackoff 计算退避时间（指数退避，最大60秒）
func (m *Monitor) calculateBackoff(attempt int, baseInterval time.Duration) time.Duration {
	// 指数退避: baseInterval * 2^(attempt-1)
	backoff := baseInterval
	for i := 1; i < attempt && i < 6; i++ {
		backoff *= 2
	}

	// 最大60秒
	maxBackoff := 60 * time.Second
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	return backoff
}


package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"autossh/internal/config"
	"autossh/internal/ssh"
)

// RemoteTunnel 远程端口转发隧道 (-R)
// 在远程服务器上监听端口，将流量转发回本地目标
type RemoteTunnel struct {
	client   *ssh.Client
	spec     config.RemoteTunnel
	listener net.Listener
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// NewRemoteTunnel 创建远程转发隧道
func NewRemoteTunnel(client *ssh.Client, spec config.RemoteTunnel) *RemoteTunnel {
	return &RemoteTunnel{
		client: client,
		spec:   spec,
	}
}

// Start 启动隧道
func (t *RemoteTunnel) Start(ctx context.Context) error {
	t.mu.Lock()

	// 在远程服务器上监听
	listener, err := t.client.Listen("tcp", t.spec.Bind)
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("远程监听失败 %s: %w", t.spec.Bind, err)
	}
	t.listener = listener
	t.mu.Unlock()

	slog.Info("远程转发已启动", "bind", t.spec.Bind, "target", t.spec.Target)

	// 接受连接
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		remoteConn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				slog.Warn("接受远程连接失败", "error", err)
				continue
			}
		}

		t.wg.Add(1)
		go t.handleConnection(ctx, remoteConn)
	}
}

// handleConnection 处理连接
func (t *RemoteTunnel) handleConnection(ctx context.Context, remoteConn net.Conn) {
	defer t.wg.Done()
	defer remoteConn.Close()

	slog.Debug("新的远程转发连接", "from", remoteConn.RemoteAddr(), "to", t.spec.Target)

	// 连接到本地目标
	localConn, err := net.Dial("tcp", t.spec.Target)
	if err != nil {
		slog.Warn("连接本地目标失败", "target", t.spec.Target, "error", err)
		return
	}
	defer localConn.Close()

	// 双向转发数据
	bidirectionalCopy(ctx, remoteConn, localConn)
}

// Stop 停止隧道
func (t *RemoteTunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.listener != nil {
		err := t.listener.Close()
		t.listener = nil
		t.wg.Wait()
		return err
	}
	return nil
}

// Type 返回隧道类型
func (t *RemoteTunnel) Type() string {
	return "remote"
}

// String 返回隧道描述
func (t *RemoteTunnel) String() string {
	return fmt.Sprintf("%s -> %s", t.spec.Bind, t.spec.Target)
}


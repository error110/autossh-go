package tunnel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"autossh/internal/config"
	"autossh/internal/ssh"
)

// LocalTunnel 本地端口转发隧道 (-L)
// 在本地监听端口，将流量通过SSH隧道转发到远程目标
type LocalTunnel struct {
	client   *ssh.Client
	spec     config.LocalTunnel
	listener net.Listener
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// NewLocalTunnel 创建本地转发隧道
func NewLocalTunnel(client *ssh.Client, spec config.LocalTunnel) *LocalTunnel {
	return &LocalTunnel{
		client: client,
		spec:   spec,
	}
}

// Start 启动隧道
func (t *LocalTunnel) Start(ctx context.Context) error {
	t.mu.Lock()

	// 在本地监听
	listener, err := net.Listen("tcp", t.spec.Bind)
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("本地监听失败 %s: %w", t.spec.Bind, err)
	}
	t.listener = listener
	t.mu.Unlock()

	slog.Info("本地转发已启动", "bind", t.spec.Bind, "target", t.spec.Target)

	// 接受连接
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				slog.Warn("接受连接失败", "error", err)
				continue
			}
		}

		t.wg.Add(1)
		go t.handleConnection(ctx, conn)
	}
}

// handleConnection 处理连接
func (t *LocalTunnel) handleConnection(ctx context.Context, localConn net.Conn) {
	defer t.wg.Done()
	defer localConn.Close()

	slog.Debug("新的本地转发连接", "from", localConn.RemoteAddr(), "to", t.spec.Target)

	// 通过SSH隧道连接到远程目标
	remoteConn, err := t.client.Dial("tcp", t.spec.Target)
	if err != nil {
		slog.Warn("连接远程目标失败", "target", t.spec.Target, "error", err)
		return
	}
	defer remoteConn.Close()

	// 双向转发数据
	bidirectionalCopy(ctx, localConn, remoteConn)
}

// Stop 停止隧道
func (t *LocalTunnel) Stop() error {
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
func (t *LocalTunnel) Type() string {
	return "local"
}

// String 返回隧道描述
func (t *LocalTunnel) String() string {
	return fmt.Sprintf("%s -> %s", t.spec.Bind, t.spec.Target)
}

// bidirectionalCopy 双向复制数据
func bidirectionalCopy(ctx context.Context, conn1, conn2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyFunc := func(dst, src net.Conn) {
		defer wg.Done()
		_, err := io.Copy(dst, src)
		if err != nil && !isClosedError(err) {
			slog.Debug("数据转发结束", "error", err)
		}
		// 关闭写入端，通知对方数据传输结束
		if tcpConn, ok := dst.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}

	go copyFunc(conn1, conn2)
	go copyFunc(conn2, conn1)

	// 等待两个方向都完成，或者上下文取消
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}

// isClosedError 检查是否是连接关闭错误
func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	return err == io.EOF || err == net.ErrClosed
}


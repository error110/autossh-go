package tunnel

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"autossh/internal/config"
	"autossh/internal/ssh"
)

const (
	// SOCKS5 版本
	socks5Version = 0x05

	// 认证方法
	authNone     = 0x00
	authPassword = 0x02
	authNoAccept = 0xFF

	// 命令类型
	cmdConnect = 0x01
	cmdBind    = 0x02
	cmdUDP     = 0x03

	// 地址类型
	addrTypeIPv4   = 0x01
	addrTypeDomain = 0x03
	addrTypeIPv6   = 0x04

	// 响应状态
	repSuccess         = 0x00
	repServerFailure   = 0x01
	repNotAllowed      = 0x02
	repNetworkUnreach  = 0x03
	repHostUnreach     = 0x04
	repConnRefused     = 0x05
	repTTLExpired      = 0x06
	repCmdNotSupported = 0x07
	repAddrNotSupported = 0x08
)

// DynamicTunnel 动态端口转发隧道 (-D)
// 实现 SOCKS5 代理协议
type DynamicTunnel struct {
	client   *ssh.Client
	spec     config.DynamicTunnel
	listener net.Listener
	mu       sync.Mutex
	wg       sync.WaitGroup
}

// NewDynamicTunnel 创建动态转发隧道
func NewDynamicTunnel(client *ssh.Client, spec config.DynamicTunnel) *DynamicTunnel {
	return &DynamicTunnel{
		client: client,
		spec:   spec,
	}
}

// Start 启动隧道
func (t *DynamicTunnel) Start(ctx context.Context) error {
	t.mu.Lock()

	// 在本地监听
	listener, err := net.Listen("tcp", t.spec.Bind)
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("SOCKS5监听失败 %s: %w", t.spec.Bind, err)
	}
	t.listener = listener
	t.mu.Unlock()

	slog.Info("SOCKS5代理已启动", "bind", t.spec.Bind)

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
				slog.Warn("接受SOCKS5连接失败", "error", err)
				continue
			}
		}

		t.wg.Add(1)
		go t.handleConnection(ctx, conn)
	}
}

// handleConnection 处理 SOCKS5 连接
func (t *DynamicTunnel) handleConnection(ctx context.Context, conn net.Conn) {
	defer t.wg.Done()
	defer conn.Close()

	// 握手阶段
	if err := t.handshake(conn); err != nil {
		slog.Debug("SOCKS5握手失败", "error", err)
		return
	}

	// 请求阶段
	targetAddr, err := t.readRequest(conn)
	if err != nil {
		slog.Debug("读取SOCKS5请求失败", "error", err)
		return
	}

	slog.Debug("SOCKS5连接请求", "from", conn.RemoteAddr(), "to", targetAddr)

	// 通过SSH隧道连接目标
	remoteConn, err := t.client.Dial("tcp", targetAddr)
	if err != nil {
		slog.Debug("连接目标失败", "target", targetAddr, "error", err)
		t.sendReply(conn, repHostUnreach, nil)
		return
	}
	defer remoteConn.Close()

	// 发送成功响应
	localAddr := conn.LocalAddr().(*net.TCPAddr)
	t.sendReply(conn, repSuccess, localAddr)

	// 双向转发数据
	bidirectionalCopy(ctx, conn, remoteConn)
}

// handshake SOCKS5 握手
func (t *DynamicTunnel) handshake(conn net.Conn) error {
	// 读取版本和认证方法数量
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return fmt.Errorf("读取握手头失败: %w", err)
	}

	if header[0] != socks5Version {
		return fmt.Errorf("不支持的SOCKS版本: %d", header[0])
	}

	// 读取认证方法列表
	numMethods := int(header[1])
	methods := make([]byte, numMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("读取认证方法失败: %w", err)
	}

	// 检查是否支持无认证
	hasNoAuth := false
	for _, m := range methods {
		if m == authNone {
			hasNoAuth = true
			break
		}
	}

	if !hasNoAuth {
		conn.Write([]byte{socks5Version, authNoAccept})
		return fmt.Errorf("客户端不支持无认证")
	}

	// 选择无认证
	_, err := conn.Write([]byte{socks5Version, authNone})
	return err
}

// readRequest 读取 SOCKS5 请求
func (t *DynamicTunnel) readRequest(conn net.Conn) (string, error) {
	// 读取请求头: VER | CMD | RSV | ATYP
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", fmt.Errorf("读取请求头失败: %w", err)
	}

	if header[0] != socks5Version {
		return "", fmt.Errorf("无效的SOCKS版本: %d", header[0])
	}

	// 只支持 CONNECT 命令
	if header[1] != cmdConnect {
		t.sendReply(conn, repCmdNotSupported, nil)
		return "", fmt.Errorf("不支持的命令: %d", header[1])
	}

	// 读取目标地址
	var host string
	addrType := header[3]

	switch addrType {
	case addrTypeIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", fmt.Errorf("读取IPv4地址失败: %w", err)
		}
		host = net.IP(addr).String()

	case addrTypeDomain:
		// 读取域名长度
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", fmt.Errorf("读取域名长度失败: %w", err)
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", fmt.Errorf("读取域名失败: %w", err)
		}
		host = string(domain)

	case addrTypeIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", fmt.Errorf("读取IPv6地址失败: %w", err)
		}
		host = net.IP(addr).String()

	default:
		t.sendReply(conn, repAddrNotSupported, nil)
		return "", fmt.Errorf("不支持的地址类型: %d", addrType)
	}

	// 读取端口
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", fmt.Errorf("读取端口失败: %w", err)
	}
	port := binary.BigEndian.Uint16(portBuf)

	return fmt.Sprintf("%s:%d", host, port), nil
}

// sendReply 发送 SOCKS5 响应
func (t *DynamicTunnel) sendReply(conn net.Conn, rep byte, addr *net.TCPAddr) {
	// VER | REP | RSV | ATYP | BND.ADDR | BND.PORT
	reply := []byte{socks5Version, rep, 0x00, addrTypeIPv4}

	if addr != nil && addr.IP != nil {
		ip := addr.IP.To4()
		if ip == nil {
			ip = net.IPv4zero.To4()
		}
		reply = append(reply, ip...)
		portBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(portBuf, uint16(addr.Port))
		reply = append(reply, portBuf...)
	} else {
		// 使用零地址
		reply = append(reply, 0, 0, 0, 0, 0, 0)
	}

	conn.Write(reply)
}

// Stop 停止隧道
func (t *DynamicTunnel) Stop() error {
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
func (t *DynamicTunnel) Type() string {
	return "dynamic"
}

// String 返回隧道描述
func (t *DynamicTunnel) String() string {
	return fmt.Sprintf("SOCKS5 %s", t.spec.Bind)
}


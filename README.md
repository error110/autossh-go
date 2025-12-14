# AutoSSH

一个纯 Go 语言实现的 SSH 隧道工具，支持自动重连功能。不依赖系统的 ssh 命令。

## 功能特性

- **本地端口转发 (-L)**: 在本地监听端口，将流量通过 SSH 隧道转发到远程目标
- **远程端口转发 (-R)**: 在远程服务器上监听端口，将流量转发回本地
- **动态端口转发 (-D)**: SOCKS5 代理，支持动态目标地址
- **自动重连**: 检测连接断开后自动重新建立连接，支持指数退避策略
- **多种认证方式**: 支持密码认证和密钥认证
- **灵活配置**: 支持命令行参数和 YAML 配置文件

## 安装

### 从源码编译

```bash
# 克隆仓库
git clone https://github.com/yourusername/autossh.git
cd autossh

# 编译
go build -o autossh.exe .

# 或者直接运行
go run .
```

### 依赖

- Go 1.21+
- golang.org/x/crypto/ssh
- github.com/spf13/cobra
- github.com/spf13/viper

## 使用方法

### 命令行模式

```bash
# 基本语法
autossh [flags] [user@]host[:port]

# 本地端口转发
autossh -L 8080:localhost:80 user@host

# 远程端口转发
autossh -R 9090:localhost:22 user@host

# SOCKS5 代理
autossh -D 1080 user@host

# 组合使用
autossh -L 8080:localhost:80 -R 9090:localhost:22 -D 1080 user@host

# 指定私钥
autossh -i ~/.ssh/id_rsa -L 8080:localhost:80 user@host

# 指定端口
autossh -p 2222 -L 8080:localhost:80 user@host

# 详细输出
autossh -v -L 8080:localhost:80 user@host
```

### 配置文件模式

```bash
# 使用配置文件
autossh -c config.yaml

# 配置文件 + 命令行参数（命令行参数优先）
autossh -c config.yaml -L 3000:localhost:3000 user@host
```

### 命令行参数

| 参数 | 短参数 | 说明 |
|------|--------|------|
| --config | -c | 配置文件路径 |
| --monitor | -M | 监控端口 (0 = 禁用) |
| --local | -L | 本地端口转发 [bind_address:]port:host:hostport |
| --remote | -R | 远程端口转发 [bind_address:]port:host:hostport |
| --dynamic | -D | 动态端口转发 (SOCKS5) [bind_address:]port |
| --port | -p | SSH 端口 (默认: 22) |
| --identity | -i | 私钥文件路径 |
| --verbose | -v | 详细输出 |
| --help | -h | 显示帮助信息 |

## 配置文件

支持 YAML 格式的配置文件，参见 `config.example.yaml`：

```yaml
server:
  host: example.com
  port: 22
  user: admin

auth:
  type: key
  key_file: ~/.ssh/id_rsa

tunnels:
  local:
    - bind: "127.0.0.1:8080"
      target: "localhost:80"
  remote:
    - bind: "0.0.0.0:9090"
      target: "localhost:22"
  dynamic:
    - bind: "127.0.0.1:1080"

reconnect:
  enabled: true
  interval: 5s
  max_retries: 0
```

## 使用示例

### 1. 访问内网 Web 服务

```bash
# 将远程服务器的 80 端口映射到本地 8080
autossh -L 8080:localhost:80 user@jumpserver

# 现在可以通过 http://localhost:8080 访问
```

### 2. 暴露本地服务到公网

```bash
# 将本地 3000 端口暴露到远程服务器的 8080 端口
autossh -R 8080:localhost:3000 user@publicserver

# 现在可以通过 http://publicserver:8080 访问本地服务
```

### 3. SOCKS5 代理翻墙

```bash
# 在本地 1080 端口启动 SOCKS5 代理
autossh -D 1080 user@proxyserver

# 配置浏览器使用 SOCKS5 代理 127.0.0.1:1080
```

### 4. 多隧道组合

```bash
# 同时建立多个隧道
autossh -L 8080:web.internal:80 \
        -L 3306:mysql.internal:3306 \
        -R 9000:localhost:22 \
        -D 1080 \
        user@jumpserver
```

## 安全注意事项

1. **主机密钥验证**: 当前版本使用 `InsecureIgnoreHostKey()`，生产环境应实现正确的主机密钥验证
2. **密码存储**: 避免在配置文件中明文存储密码，推荐使用密钥认证
3. **权限控制**: 确保配置文件和私钥文件权限正确 (chmod 600)

## 与原版 autossh 的区别

| 特性 | 本项目 | 原版 autossh |
|------|--------|--------------|
| 依赖 | 纯 Go，无外部依赖 | 需要系统 ssh 命令 |
| 跨平台 | 编译为单一可执行文件 | 依赖系统环境 |
| 配置 | 支持 YAML 配置文件 | 仅命令行参数 |
| 监控 | 内置心跳检测 | 需要 -M 端口 |

## License

MIT License


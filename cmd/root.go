package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"autossh/internal/config"
	"autossh/internal/monitor"
	"autossh/internal/ssh"
	"autossh/internal/tunnel"

	"github.com/spf13/cobra"
)

var (
	cfgFile       string
	monitorPort   int
	localForwards []string
	remoteForwards []string
	dynamicForwards []string
	sshPort       int
	identityFile  string
	verbose       bool
)

// rootCmd 根命令
var rootCmd = &cobra.Command{
	Use:   "autossh [flags] [user@]host[:port]",
	Short: "自动重连的SSH隧道工具",
	Long: `autossh 是一个纯Go实现的SSH隧道工具，支持：
- 本地端口转发 (-L)
- 远程端口转发 (-R)  
- 动态端口转发/SOCKS5代理 (-D)
- 自动检测断线并重连
- 密码和密钥认证`,
	Example: `  # 本地端口转发
  autossh -L 8080:localhost:80 user@host

  # 远程端口转发
  autossh -R 9090:localhost:22 user@host

  # SOCKS5 代理
  autossh -D 1080 user@host

  # 使用配置文件
  autossh -c config.yaml

  # 组合使用
  autossh -L 8080:localhost:80 -D 1080 -i ~/.ssh/id_rsa user@host`,
	Args: cobra.MaximumNArgs(1),
	RunE: run,
}

func init() {
	rootCmd.Flags().StringVarP(&cfgFile, "config", "c", "", "配置文件路径")
	rootCmd.Flags().IntVarP(&monitorPort, "monitor", "M", 0, "监控端口 (0 = 禁用, 使用 ServerAliveInterval)")
	rootCmd.Flags().StringArrayVarP(&localForwards, "local", "L", nil, "本地端口转发 [bind_address:]port:host:hostport")
	rootCmd.Flags().StringArrayVarP(&remoteForwards, "remote", "R", nil, "远程端口转发 [bind_address:]port:host:hostport")
	rootCmd.Flags().StringArrayVarP(&dynamicForwards, "dynamic", "D", nil, "动态端口转发 (SOCKS5) [bind_address:]port")
	rootCmd.Flags().IntVarP(&sshPort, "port", "p", 22, "SSH端口")
	rootCmd.Flags().StringVarP(&identityFile, "identity", "i", "", "私钥文件路径")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "详细输出")
}

// Execute 执行根命令
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// 设置日志级别
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// 加载配置
	cfg, err := loadConfig(args)
	if err != nil {
		return err
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("配置验证失败: %w", err)
	}

	slog.Info("启动 autossh",
		"server", cfg.Address(),
		"user", cfg.Server.User,
		"auth", cfg.Auth.Type,
	)

	// 创建SSH客户端
	client := ssh.NewClient(cfg)

	// 创建隧道管理器
	tunnelMgr := tunnel.NewManager(client, cfg)

	// 创建监控器
	mon := monitor.NewMonitor(client, tunnelMgr, cfg)

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动监控器 (包含连接和隧道管理)
	errChan := make(chan error, 1)
	go func() {
		errChan <- mon.Start()
	}()

	// 等待退出信号或错误
	select {
	case sig := <-sigChan:
		slog.Info("收到信号，正在退出...", "signal", sig)
		mon.Stop()
	case err := <-errChan:
		if err != nil {
			return err
		}
	}

	return nil
}

// loadConfig 从命令行参数和配置文件加载配置
func loadConfig(args []string) (*config.Config, error) {
	var cfg *config.Config
	var err error

	// 尝试加载配置文件
	if cfgFile != "" {
		cfg, err = config.LoadFromFile(cfgFile)
		if err != nil {
			return nil, err
		}
	} else {
		cfg = config.DefaultConfig()
	}

	// 命令行参数覆盖配置文件
	if len(args) > 0 {
		user, host, port, err := config.ParseTarget(args[0])
		if err != nil {
			return nil, err
		}
		if user != "" {
			cfg.Server.User = user
		}
		cfg.Server.Host = host
		if port != 22 {
			cfg.Server.Port = port
		}
	}

	// 命令行端口覆盖
	if sshPort != 22 {
		cfg.Server.Port = sshPort
	}

	// 密钥文件
	if identityFile != "" {
		cfg.Auth.Type = "key"
		cfg.Auth.KeyFile = identityFile
	}

	// 解析本地转发
	for _, spec := range localForwards {
		tunnel, err := config.ParseLocalForward(spec)
		if err != nil {
			return nil, err
		}
		cfg.Tunnels.Local = append(cfg.Tunnels.Local, *tunnel)
	}

	// 解析远程转发
	for _, spec := range remoteForwards {
		tunnel, err := config.ParseRemoteForward(spec)
		if err != nil {
			return nil, err
		}
		cfg.Tunnels.Remote = append(cfg.Tunnels.Remote, *tunnel)
	}

	// 解析动态转发
	for _, spec := range dynamicForwards {
		tunnel, err := config.ParseDynamicForward(spec)
		if err != nil {
			return nil, err
		}
		cfg.Tunnels.Dynamic = append(cfg.Tunnels.Dynamic, *tunnel)
	}

	// 如果没有指定用户名，使用当前系统用户
	if cfg.Server.User == "" {
		cfg.Server.User = os.Getenv("USER")
		if cfg.Server.User == "" {
			cfg.Server.User = os.Getenv("USERNAME")
		}
	}

	return cfg, nil
}


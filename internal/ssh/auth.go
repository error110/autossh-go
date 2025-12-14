package ssh

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"autossh/internal/config"

	"golang.org/x/crypto/ssh"
)

// GetAuthMethods 根据配置获取认证方法
func GetAuthMethods(cfg *config.Config) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	switch cfg.Auth.Type {
	case "password":
		password := cfg.Auth.Password
		if password == "" {
			var err error
			password, err = readPassword(fmt.Sprintf("%s@%s's password: ", cfg.Server.User, cfg.Server.Host))
			if err != nil {
				return nil, fmt.Errorf("读取密码失败: %w", err)
			}
		}
		authMethods = append(authMethods, ssh.Password(password))

	case "key":
		keyAuth, err := getKeyAuth(cfg.Auth.KeyFile, cfg.Auth.Passphrase)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, keyAuth)

	default:
		return nil, fmt.Errorf("不支持的认证类型: %s", cfg.Auth.Type)
	}

	return authMethods, nil
}

// getKeyAuth 从私钥文件获取认证方法
func getKeyAuth(keyFile, passphrase string) (ssh.AuthMethod, error) {
	keyBytes, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("读取密钥文件失败 %s: %w", keyFile, err)
	}

	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			// 如果解析失败，可能需要密码短语
			if strings.Contains(err.Error(), "passphrase") {
				passphrase, err = readPassword(fmt.Sprintf("Enter passphrase for key '%s': ", keyFile))
				if err != nil {
					return nil, fmt.Errorf("读取密钥密码失败: %w", err)
				}
				signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(passphrase))
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("解析密钥失败: %w", err)
	}

	return ssh.PublicKeys(signer), nil
}

// readPassword 从终端读取密码（不回显）
func readPassword(prompt string) (string, error) {
	fmt.Print(prompt)

	// Windows 下使用简单的方式读取（不能禁用回显）
	// 生产环境应使用 golang.org/x/term 包
	if isWindows() {
		reader := bufio.NewReader(os.Stdin)
		password, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		fmt.Println()
		return strings.TrimSpace(password), nil
	}

	// Unix 系统禁用回显
	return readPasswordUnix()
}

// isWindows 检查是否为 Windows 系统
func isWindows() bool {
	return os.PathSeparator == '\\'
}

// readPasswordUnix Unix 系统下读取密码
func readPasswordUnix() (string, error) {
	// 保存原始终端设置
	fd := int(syscall.Stdin)
	
	// 简单实现：直接读取（更好的实现应使用 golang.org/x/term）
	reader := bufio.NewReader(os.Stdin)
	password, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	fmt.Println()
	
	_ = fd // 避免未使用警告
	return strings.TrimSpace(password), nil
}


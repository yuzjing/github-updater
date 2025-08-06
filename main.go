package main

import (
	"bytes"
	"encoding/json"
	"flag" // 导入 flag 包
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"text/template"
)

// NftablesConfig, GitHubMeta, nftTemplate ... 这些结构体和常量保持不变
type NftablesConfig struct {
	Family      string
	TableName   string
	IPv4SetName string
	IPv6SetName string
	IPv4Addrs   string
	IPv6Addrs   string
}
type GitHubMeta struct {
	Actions []string `json:"actions"`
}
const nftTemplate = `
flush set {{.Family}} {{.TableName}} {{.IPv4SetName}}
add element {{.Family}} {{.TableName}} {{.IPv4SetName}} { {{.IPv4Addrs}} }
flush set {{.Family}} {{.TableName}} {{.IPv6SetName}}
add element {{.Family}} {{.TableName}} {{.IPv6SetName}} { {{.IPv6Addrs}} }
`

var verbose bool // 定义一个全局变量来存储是否启用详细模式

// logVerbose 是一个新的帮助函数，只在 verbose 模式下打印日志
func logVerbose(format string, v ...interface{}) {
	if verbose {
		log.Printf(format, v...)
	}
}

func main() {
	// 定义 -v 标志。当用户在命令行提供 -v 时，verbose 变量会变为 true
	flag.BoolVar(&verbose, "v", false, "Enable verbose output for debugging.")
	flag.Parse()

	logVerbose("Starting GitHub Actions IP update process...")

	// 1. 获取数据
	meta, err := fetchGitHubMeta()
	if err != nil {
		log.Fatalf("ERROR: Failed to fetch GitHub meta data: %v", err)
	}
	logVerbose("Successfully fetched %d IP ranges from GitHub.", len(meta.Actions))

	// 2. 分类 IP
	var ipv4s, ipv6s []string
	for _, cidr := range meta.Actions {
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("Warning: Could not parse CIDR %s. Skipping.", cidr)
			continue
		}
		if ip.To4() != nil {
			ipv4s = append(ipv4s, cidr)
		} else {
			ipv6s = append(ipv6s, cidr)
		}
	}
	logVerbose("Found %d IPv4 ranges and %d IPv6 ranges.", len(ipv4s), len(ipv6s))

	if len(ipv4s) == 0 || len(ipv6s) == 0 {
		log.Fatalf("ERROR: Did not find both IPv4 and IPv6 addresses. Aborting to be safe.")
	}

	// 3. 填充配置
	config := NftablesConfig{
		Family:      "inet",
		TableName:   "filter",
		IPv4SetName: "github_actions_ipv4",
		IPv6SetName: "github_actions_ipv6",
		IPv4Addrs:   strings.Join(ipv4s, ", "),
		IPv6Addrs:   strings.Join(ipv6s, ", "),
	}

	// 4. 生成命令
	commands, err := generateNftCommands(config)
	if err != nil {
		log.Fatalf("ERROR: Failed to generate nftables commands: %v", err)
	}

	// 5. 执行命令
	if err := executeNftCommands(commands); err != nil {
		log.Fatalf("ERROR: Failed to execute nftables commands: %v", err)
	}

	// 这是唯一在默认模式下成功时会打印的消息
	log.Println("Successfully updated nftables sets for GitHub Actions.")
}

// executeNftCommands 现在也使用 logVerbose
func executeNftCommands(commands string) error {
	logVerbose("Preparing to execute nft commands...")
	
	cmd := exec.Command("sudo", "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(commands)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft command failed: %v\nOutput:\n%s", err, string(output))
	}
	
	// 只在详细模式下打印 nft 的成功输出
	logVerbose("nft command output: %s", string(output))
	return nil
}

// fetchGitHubMeta 和 generateNftCommands 函数保持不变
func fetchGitHubMeta() (*GitHubMeta, error) {
    resp, err := http.Get("https://api.github.com/meta")
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("bad status from GitHub API: %s", resp.Status)
    }
    var meta GitHubMeta
    if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
        return nil, err
    }
    return &meta, nil
}
func generateNftCommands(config NftablesConfig) (string, error) {
    tmpl, err := template.New("nft").Parse(strings.TrimSpace(nftTemplate))
    if err != nil {
        return "", err
    }
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, config); err != nil {
        return "", err
    }
    return buf.String(), nil
}

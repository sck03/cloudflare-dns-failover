package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type notification struct {
	message string
	config  NotificationConfig
}

type Notifier struct {
	queue  chan notification
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var notifier *Notifier
var notifyClient = &http.Client{Timeout: 10 * time.Second}

func NewNotifier() *Notifier {
	ctx, cancel := context.WithCancel(context.Background())
	n := &Notifier{queue: make(chan notification, 64), ctx: ctx, cancel: cancel}
	for range 2 {
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			for {
				select {
				case <-n.ctx.Done():
					return
				case item := <-n.queue:
					n.deliver(item)
				}
			}
		}()
	}
	return n
}

func SendNotification(message string) {
	if notifier == nil {
		return
	}
	config := ConfigSnapshot().Notification
	if !config.DingTalk.Enabled && !config.Telegram.Enabled && !config.Email.Enabled {
		return
	}
	select {
	case <-notifier.ctx.Done():
	case notifier.queue <- notification{message: message, config: config}:
	default:
		log.Print("Notification queue full; event remains available in monitor history")
	}
}

func (n *Notifier) Stop() {
	n.cancel()
	n.wg.Wait()
	notifyClient.CloseIdleConnections()
}

func (n *Notifier) deliver(item notification) {
	config := item.config
	if config.DingTalk.Enabled {
		address := "https://oapi.dingtalk.com/robot/send?access_token=" + url.QueryEscape(config.DingTalk.AccessToken)
		if config.DingTalk.Secret != "" {
			timestamp := time.Now().UnixMilli()
			mac := hmac.New(sha256.New, []byte(config.DingTalk.Secret))
			fmt.Fprintf(mac, "%d\n%s", timestamp, config.DingTalk.Secret)
			address += fmt.Sprintf("&timestamp=%d&sign=%s", timestamp, url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil))))
		}
		if err := postNotification(n.ctx, address, map[string]any{
			"msgtype": "text", "text": map[string]string{"content": "CFGuard: " + item.message},
		}, false); err != nil {
			log.Printf("DingTalk notification: %v", err)
		}
	}
	if config.Telegram.Enabled {
		address := "https://api.telegram.org/bot" + config.Telegram.BotToken + "/sendMessage"
		if err := postNotification(n.ctx, address, map[string]string{
			"chat_id": config.Telegram.ChatID, "text": "CFGuard: " + item.message,
		}, true); err != nil {
			log.Printf("Telegram notification: %v", err)
		}
	}
	if config.Email.Enabled {
		if err := sendEmail(n.ctx, config, item.message); err != nil {
			log.Printf("Email notification: %v", err)
		}
	}
}

func postNotification(ctx context.Context, address string, payload any, telegram bool) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid notification endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := notifyClient.Do(req)
	if err != nil {
		// URL errors can contain bot credentials; never log the request URL.
		return fmt.Errorf("notification request failed")
	}
	defer resp.Body.Close()
	var result struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"errcode"`
		Message     string `json:"errmsg"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&result); err != nil {
		return fmt.Errorf("invalid notification response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || telegram && !result.OK || !telegram && result.ErrorCode != 0 {
		return fmt.Errorf("provider rejected notification (HTTP %d, code %d)", resp.StatusCode, result.ErrorCode)
	}
	return nil
}

func sendEmail(ctx context.Context, config NotificationConfig, message string) error {
	conf := config.Email
	if strings.ContainsAny(conf.To+conf.Username, "\r\n") {
		return fmt.Errorf("invalid email address")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(conf.Host, strconv.Itoa(conf.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	tlsConfig := &tls.Config{ServerName: conf.Host, MinVersion: tls.VersionTLS12}
	var smtpConn net.Conn = conn
	if conf.Port == 465 {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		smtpConn = secure
	}
	client, err := smtp.NewClient(smtpConn, conf.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if conf.Port != 465 {
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if err := client.Auth(smtp.PlainAuth("", conf.Username, conf.Password, conf.Host)); err != nil {
		return err
	}
	if err := client.Mail(conf.Username); err != nil {
		return err
	}
	if err := client.Rcpt(conf.To); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nSubject: CFGuard Notification\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", conf.Username, conf.To, message)
	if err != nil {
		writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

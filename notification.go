package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"time"
)

// --- Notification Service ---

func SendNotification(message string) {
	// DingTalk
	if AppConfig.Notification.DingTalk.Enabled {
		go sendDingTalk(message)
	}

	// Telegram
	if AppConfig.Notification.Telegram.Enabled {
		go sendTelegram(message)
	}

	// Email
	if AppConfig.Notification.Email.Enabled {
		go sendEmail(message)
	}
}

var notifyClient = &http.Client{
	Timeout: 10 * time.Second,
}

func sendDingTalk(content string) {
	token := AppConfig.Notification.DingTalk.AccessToken
	secret := AppConfig.Notification.DingTalk.Secret
	if token == "" {
		return
	}

	apiUrl := "https://oapi.dingtalk.com/robot/send?access_token=" + token

	// Sign if secret is present
	if secret != "" {
		timestamp := time.Now().UnixNano() / 1e6
		stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
		// DingTalk signature needs URL encoding
		apiUrl += fmt.Sprintf("&timestamp=%d&sign=%s", timestamp, url.QueryEscape(sign))
	}

	payload := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]string{
			"content": "CFGuard: " + content,
		},
	}
	jsonPayload, _ := json.Marshal(payload)

	resp, err := notifyClient.Post(apiUrl, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("DingTalk notification failed: %v", err)
	} else {
		defer resp.Body.Close()
	}
}

func sendTelegram(content string) {
	token := AppConfig.Notification.Telegram.BotToken
	chatId := AppConfig.Notification.Telegram.ChatID
	if token == "" || chatId == "" {
		return
	}
	apiUrl := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]string{
		"chat_id": chatId,
		"text":    "CFGuard: " + content,
	}
	jsonPayload, _ := json.Marshal(payload)

	resp, err := notifyClient.Post(apiUrl, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Telegram notification failed: %v", err)
	} else {
		defer resp.Body.Close()
	}
}

func sendEmail(content string) {
	conf := AppConfig.Notification.Email
	if !conf.Enabled {
		return
	}

	addr := fmt.Sprintf("%s:%d", conf.Host, conf.Port)

	// Message Construction
	subject := "CFGuard Notification"
	body := "To: " + conf.To + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		content + "\r\n"

	msg := []byte(body)

	auth := smtp.PlainAuth("", conf.Username, conf.Password, conf.Host)

	var err error
	if conf.Port == 465 {
		// Implicit TLS (SMTPS)
		// TLS Connection
		tlsConfig := &tls.Config{
			ServerName:         conf.Host,
			InsecureSkipVerify: false, // Set to true only for self-signed certs if needed
		}

		dialer := &net.Dialer{Timeout: 10 * time.Second}
		conn, tlsErr := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if tlsErr != nil {
			log.Println("Failed to dial TLS for email:", tlsErr)
			return
		}

		c, smtpErr := smtp.NewClient(conn, conf.Host)
		if smtpErr != nil {
			conn.Close()
			log.Println("Failed to create SMTP client:", smtpErr)
			return
		}
		defer c.Quit()

		if err = c.Auth(auth); err != nil {
			log.Println("SMTP Auth failed:", err)
			return
		}
		if err = c.Mail(conf.Username); err != nil {
			log.Println("SMTP Mail failed:", err)
			return
		}
		if err = c.Rcpt(conf.To); err != nil {
			log.Println("SMTP Rcpt failed:", err)
			return
		}
		w, err := c.Data()
		if err != nil {
			log.Println("SMTP Data failed:", err)
			return
		}
		_, err = w.Write(msg)
		if err != nil {
			log.Println("SMTP Write failed:", err)
			return
		}
		err = w.Close()
		if err != nil {
			log.Println("SMTP Close failed:", err)
			return
		}
	} else {
		// STARTTLS or Plain (587 or 25)
		err = smtp.SendMail(addr, auth, conf.Username, []string{conf.To}, msg)
		if err != nil {
			log.Printf("Failed to send email: %v", err)
		}
	}
}

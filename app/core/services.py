import CloudFlare
import requests
import smtplib
from email.mime.text import MIMEText
from email.mime.multipart import MIMEMultipart
from email.header import Header
import time
import json
import logging
import hashlib
import hmac
import base64
import urllib.parse
from datetime import datetime

class CloudflareService:
    def __init__(self, api_token=None, api_key=None, email=None):
        try:
            if api_token:
                self.cf = CloudFlare.CloudFlare(token=api_token)
            elif api_key and email:
                self.cf = CloudFlare.CloudFlare(email=email, key=api_key)
            else:
                raise ValueError("Missing credentials")
        except Exception as e:
            logging.error(f"Failed to init Cloudflare: {e}")
            self.cf = None

    def list_zones(self):
        if not self.cf: return []
        try:
            return self.cf.zones.get()
        except Exception as e:
            logging.error(f"CF list_zones error: {e}")
            return []

    def list_records(self, zone_id):
        if not self.cf: return []
        try:
            return self.cf.zones.dns_records.get(zone_id)
        except Exception as e:
            logging.error(f"CF list_records error: {e}")
            return []
            
    def create_record(self, zone_id, data):
        if not self.cf: return None
        try:
            return self.cf.zones.dns_records.post(zone_id, data=data)
        except Exception as e:
            raise e

    def update_record(self, zone_id, record_id, data):
        if not self.cf: return None
        try:
            return self.cf.zones.dns_records.put(zone_id, record_id, data=data)
        except Exception as e:
            raise e

    def delete_record(self, zone_id, record_id):
        if not self.cf: return None
        try:
            return self.cf.zones.dns_records.delete(zone_id, record_id)
        except Exception as e:
            raise e

    def update_record_by_subdomain(self, zone_id, subdomain, ip, proxied=False):
        if not self.cf: return
        try:
            # Find record first
            params = {'name': subdomain}
            records = self.cf.zones.dns_records.get(zone_id, params=params)
            
            if not records:
                logging.error(f"No DNS record found for {subdomain}")
                return

            record = records[0]
            record_id = record['id']
            
            data = {
                'type': 'A',
                'name': subdomain,
                'content': ip,
                'proxied': proxied
            }
            
            self.cf.zones.dns_records.put(zone_id, record_id, data=data)
            logging.info(f"Updated DNS record {subdomain} to {ip}")
            
        except Exception as e:
            logging.error(f"Failed to update record by subdomain: {e}")
            raise e

class NotificationService:
    def __init__(self, ding_config=None, email_config=None, telegram_config=None):
        self.ding = ding_config or {}
        self.email = email_config or {}
        self.telegram = telegram_config or {}

    def notify(self, message):
        self.send_dingtalk(message)
        self.send_email(message)
        self.send_telegram(message)

    def send_dingtalk(self, message):
        if not self.ding.get('enabled') or not self.ding.get('access_token'):
            return

        token = self.ding['access_token']
        secret = self.ding.get('secret')
        
        url = f"https://oapi.dingtalk.com/robot/send?access_token={token}"
        
        if secret:
            timestamp = str(round(time.time() * 1000))
            secret_enc = secret.encode('utf-8')
            string_to_sign = '{}\n{}'.format(timestamp, secret)
            string_to_sign_enc = string_to_sign.encode('utf-8')
            hmac_code = hmac.new(secret_enc, string_to_sign_enc, digestmod=hashlib.sha256).digest()
            sign = urllib.parse.quote_plus(base64.b64encode(hmac_code))
            url = f"{url}&timestamp={timestamp}&sign={sign}"

        payload = {
            "msgtype": "text",
            "text": {
                "content": message
            }
        }
        
        try:
            requests.post(url, json=payload, timeout=5)
        except Exception as e:
            logging.error(f"DingTalk error: {e}")

    def send_telegram(self, message):
        if not self.telegram.get('enabled') or not self.telegram.get('bot_token') or not self.telegram.get('chat_id'):
            return
            
        token = self.telegram['bot_token']
        chat_id = self.telegram['chat_id']
        url = f"https://api.telegram.org/bot{token}/sendMessage"
        
        payload = {
            "chat_id": chat_id,
            "text": message
        }
        
        try:
            requests.post(url, json=payload, timeout=5)
        except Exception as e:
            logging.error(f"Telegram error: {e}")

    def send_email(self, message):
        if not self.email.get('enabled'):
            return
            
        host = self.email.get('host')
        port = self.email.get('port')
        username = self.email.get('username')
        password = self.email.get('password')
        to_addr = self.email.get('to')
        
        if not all([host, port, username, password, to_addr]):
            return

        subject = "DNS 故障切换通知"
        
        # Build HTML content similar to Go version
        html_content = f"""
        <html>
        <body>
            <h3>DNS 故障切换通知</h3>
            <p>{message}</p>
            <p>时间: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}</p>
        </body>
        </html>
        """
        
        msg = MIMEMultipart()
        msg['From'] = username
        msg['To'] = to_addr
        msg['Subject'] = Header(subject, 'utf-8')
        msg.attach(MIMEText(html_content, 'html', 'utf-8'))
        
        try:
            if port == 465:
                server = smtplib.SMTP_SSL(host, port)
            else:
                server = smtplib.SMTP(host, port)
                # server.starttls() # Optional depending on server
            
            server.login(username, password)
            server.sendmail(username, to_addr.split(','), msg.as_string())
            server.quit()
        except Exception as e:
            logging.error(f"Email error: {e}")

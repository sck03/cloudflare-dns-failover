from . import db
from datetime import datetime
import json

class Monitor(db.Model):
    __tablename__ = 'monitors'
    
    id = db.Column(db.String(64), primary_key=True)
    name = db.Column(db.String(128))
    zone_id = db.Column(db.String(128))
    subdomains = db.Column(db.Text)  # Stored as JSON string
    check_type = db.Column(db.String(20), default='ping')
    check_target = db.Column(db.String(128))
    original_ip = db.Column(db.String(64))
    backup_ip = db.Column(db.String(64))
    failure_threshold = db.Column(db.Integer, default=3)
    success_threshold = db.Column(db.Integer, default=3)
    ping_count = db.Column(db.Integer, default=5)
    interval = db.Column(db.Integer, default=60)
    timeout_seconds = db.Column(db.Integer, default=2)
    original_ip_cdn_enabled = db.Column(db.Boolean, default=False)
    backup_ip_cdn_enabled = db.Column(db.Boolean, default=False)
    
    # Schedule fields
    schedule_enabled = db.Column(db.Boolean, default=False)
    schedule_hours = db.Column(db.Integer, default=0)
    schedule_switch_ip = db.Column(db.String(64))
    
    # Runtime status (can be updated by engine)
    status = db.Column(db.String(20), default='Normal')
    current_ip = db.Column(db.String(64))
    fail_count = db.Column(db.Integer, default=0)
    succ_count = db.Column(db.Integer, default=0)
    
    def to_dict(self):
        return {
            "id": self.id,
            "name": self.name,
            "zone_id": self.zone_id,
            "subdomains": json.loads(self.subdomains) if self.subdomains else [],
            "check_type": self.check_type,
            "check_target": self.check_target,
            "original_ip": self.original_ip,
            "backup_ip": self.backup_ip,
            "failure_threshold": self.failure_threshold,
            "success_threshold": self.success_threshold,
            "ping_count": self.ping_count,
            "interval": self.interval,
            "timeout_seconds": self.timeout_seconds,
            "original_ip_cdn_enabled": self.original_ip_cdn_enabled,
            "backup_ip_cdn_enabled": self.backup_ip_cdn_enabled,
            "schedule_enabled": self.schedule_enabled,
            "schedule_hours": self.schedule_hours,
            "schedule_switch_ip": self.schedule_switch_ip,
            "status": self.status,
            "current_ip": self.current_ip,
            "fail_count": self.fail_count,
            "succ_count": self.succ_count
        }

class SwitchLog(db.Model):
    __tablename__ = 'switch_logs'
    
    id = db.Column(db.Integer, primary_key=True)
    monitor_id = db.Column(db.String(64))
    name = db.Column(db.String(128))
    from_ip = db.Column(db.String(64))
    to_ip = db.Column(db.String(64))
    to_backup = db.Column(db.Boolean)
    check_type = db.Column(db.String(20))
    reason = db.Column(db.String(255))
    timestamp = db.Column(db.DateTime, default=datetime.utcnow)

    def to_dict(self):
        return {
            "timestamp": int(self.timestamp.timestamp() * 1000),
            "monitor_id": self.monitor_id,
            "name": self.name,
            "from_ip": self.from_ip,
            "to_ip": self.to_ip,
            "to_backup": self.to_backup,
            "check_type": self.check_type,
            "reason": self.reason
        }

class IPDownEvent(db.Model):
    __tablename__ = 'ip_down_events'
    
    id = db.Column(db.Integer, primary_key=True)
    monitor_id = db.Column(db.String(64))
    name = db.Column(db.String(128))
    ip = db.Column(db.String(64))
    role = db.Column(db.String(20)) # original, backup
    timestamp = db.Column(db.DateTime, default=datetime.utcnow)

    def to_dict(self):
        return {
            "timestamp": int(self.timestamp.timestamp() * 1000),
            "monitor_id": self.monitor_id,
            "name": self.name,
            "ip": self.ip,
            "role": self.role
        }

class CloudflareAccount(db.Model):
    __tablename__ = 'cloudflare_accounts'
    
    id = db.Column(db.String(64), primary_key=True)
    name = db.Column(db.String(128))
    api_token = db.Column(db.String(255))
    api_key = db.Column(db.String(255))
    email = db.Column(db.String(255))

    def to_dict(self):
        return {
            "id": self.id,
            "name": self.name,
            "api_token": self.api_token,
            "api_key": self.api_key,
            "email": self.email
        }

class GlobalConfig(db.Model):
    __tablename__ = 'global_config'
    
    key = db.Column(db.String(50), primary_key=True)
    value = db.Column(db.Text) # JSON value

import logging
import json
import time
from datetime import datetime
from app import db, scheduler
from app.models import Monitor, SwitchLog, GlobalConfig, IPDownEvent
from app.core.checker import check_ping, check_http
from app.core.services import CloudflareService, NotificationService

# Global reference to app for context in threads
_app = None

def init_engine(app):
    global _app
    _app = app

def get_services():
    # Helper to get configured services
    # Assumes inside app context
    gc = GlobalConfig.query.all()
    config_map = {item.key: json.loads(item.value) for item in gc if item.value}
    
    cf_config = config_map.get('cloudflare', {})
    cf_service = CloudflareService(
        api_token=cf_config.get('api_token'),
        api_key=cf_config.get('api_key'),
        email=cf_config.get('email')
    )
    
    notify_service = NotificationService(
        ding_config=config_map.get('dingtalk'),
        email_config=config_map.get('email'),
        telegram_config=config_map.get('telegram')
    )
    return cf_service, notify_service

def check_monitor_job(monitor_id):
    if not _app:
        logging.error("Engine not initialized with app")
        return

    with _app.app_context():
        m = Monitor.query.get(monitor_id)
        if not m:
            return

        # Check logic
        success = False
        if m.check_type in ['http', 'https']:
            success = check_http(m.check_target, timeout=m.timeout_seconds)
        else:
            success = check_ping(m.check_target, count=m.ping_count, timeout=m.timeout_seconds)

        if success:
            handle_success(m)
        else:
            handle_failure(m)
            
        # Check backup health if failover is active
        if m.status == 'Down' and m.check_type == 'ping' and m.backup_ip:
            check_backup_health(m)
            
        db.session.commit()

def handle_success(m):
    if m.status == 'Down':
        m.succ_count += 1
        logging.info(f"Monitor {m.name}: success count {m.succ_count}/{m.success_threshold}")
        
        if m.succ_count >= m.success_threshold:
            # Restore
            do_switch(m, to_backup=False)
            m.status = 'Normal'
            m.current_ip = m.original_ip
            m.succ_count = 0
    else:
        m.fail_count = 0

def handle_failure(m):
    if m.status == 'Normal':
        m.fail_count += 1
        logging.info(f"Monitor {m.name}: failure count {m.fail_count}/{m.failure_threshold}")
        
        if m.fail_count >= m.failure_threshold:
            # Failover
            record_ip_down(m, m.original_ip, "original")
            do_switch(m, to_backup=True)
            m.status = 'Down'
            m.current_ip = m.backup_ip
            m.fail_count = 0
    else:
        m.succ_count = 0

def do_switch(m, to_backup):
    cf, notify = get_services()
    
    target_ip = m.backup_ip if to_backup else m.original_ip
    proxied = m.backup_ip_cdn_enabled if to_backup else m.original_ip_cdn_enabled
    
    if not target_ip:
        logging.error(f"Target IP is empty for monitor {m.name}")
        return

    # Update Cloudflare
    try:
        subdomains = json.loads(m.subdomains) if m.subdomains else []
        for sub in subdomains:
            cf.update_record_by_subdomain(m.zone_id, sub, target_ip, proxied=proxied)
    except Exception as e:
        logging.error(f"Failed to update DNS for {m.name}: {e}")
        # Note: We continue to record the event even if DNS fails, or maybe we shouldn't?
        # Standard behavior: try best effort.

    # Record Event
    event = SwitchLog(
        monitor_id=m.id,
        name=m.name,
        from_ip=m.backup_ip if not to_backup else m.original_ip,
        to_ip=target_ip,
        to_backup=to_backup,
        check_type=m.check_type,
        reason='failover' if to_backup else 'restore'
    )
    db.session.add(event)
    
    # Notify
    direction = "主 -> 备" if to_backup else "备 -> 主"
    msg = f"监控【{m.name}】发生切换\n方向: {direction}\nIP: {target_ip}\n时间: {datetime.now().strftime('%H:%M:%S')}"
    notify.notify(msg)

def check_backup_health(m):
    # Logic to check backup IP when it's in use or just standby? 
    # Original Go code checks backup health when status is Down (Failover active) 
    # to alert if backup also dies.
    success = check_ping(m.backup_ip, count=m.ping_count, timeout=m.timeout_seconds)
    if not success:
        record_ip_down(m, m.backup_ip, "backup")

def record_ip_down(m, ip, role):
    # Debounce logic could be here, but for now just record
    event = IPDownEvent(
        monitor_id=m.id,
        name=m.name,
        ip=ip,
        role=role
    )
    db.session.add(event)
    
    # Notify if needed (Go code has some logic for this)
    # For simplicity, we skip separate notification for IP down unless it triggers switch

def scheduled_switch_job(monitor_id):
    if not _app: return
    with _app.app_context():
        m = Monitor.query.get(monitor_id)
        if not m or not m.schedule_enabled:
            return
            
        # Avoid interfering while failover is active
        if m.status == 'Down':
            return
            
        from_ip = m.current_ip
        to_ip = ""
        
        if m.schedule_switch_ip:
            to_ip = m.schedule_switch_ip
        elif from_ip == m.original_ip:
            to_ip = m.backup_ip
        else:
            to_ip = m.original_ip
            
        if not to_ip or to_ip == from_ip:
            return
            
        # Execute switch (without changing status to Down)
        # We need a variant of do_switch that doesn't imply failover
        
        cf, notify = get_services()
        
        # Determine if we are switching TO backup role (just for logic, though schedule is usually active/active)
        # If to_ip is backup_ip, then to_backup=True
        is_backup = (to_ip == m.backup_ip)
        proxied = m.backup_ip_cdn_enabled if is_backup else m.original_ip_cdn_enabled
        
        try:
            subdomains = json.loads(m.subdomains) if m.subdomains else []
            for sub in subdomains:
                cf.update_record_by_subdomain(m.zone_id, sub, to_ip, proxied=proxied)
        except Exception as e:
            logging.error(f"Schedule switch failed for {m.name}: {e}")
            return

        m.current_ip = to_ip
        m.fail_count = 0
        m.succ_count = 0
        
        event = SwitchLog(
            monitor_id=m.id,
            name=m.name,
            from_ip=from_ip,
            to_ip=to_ip,
            to_backup=is_backup,
            check_type=m.check_type,
            reason='schedule'
        )
        db.session.add(event)
        db.session.commit()
        
        msg = f"监控【{m.name}】计划任务切换\nIP: {from_ip} -> {to_ip}"
        notify.notify(msg)

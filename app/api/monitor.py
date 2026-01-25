from flask import Blueprint, request, jsonify
from app import db, scheduler
from app.models import Monitor
from app.utils import require_auth
from app.core.engine import check_monitor_job, scheduled_switch_job
import json
import time

bp = Blueprint('monitor', __name__, url_prefix='/api/monitors')

@bp.route('', methods=['GET'])
@require_auth
def list_monitors():
    monitors = Monitor.query.all()
    return jsonify({'code': 200, 'data': [m.to_dict() for m in monitors]})

@bp.route('', methods=['POST'])
@require_auth
def add_monitor():
    data = request.json
    m = Monitor()
    m.id = data.get('id') or str(int(time.time()*1000))
    m.name = data.get('name')
    m.zone_id = data.get('zone_id')
    m.subdomains = json.dumps(data.get('subdomains', []))
    m.check_type = data.get('check_type', 'ping')
    m.check_target = data.get('check_target')
    m.original_ip = data.get('original_ip')
    m.backup_ip = data.get('backup_ip')
    m.failure_threshold = int(data.get('failure_threshold', 3))
    m.success_threshold = int(data.get('success_threshold', 3))
    m.ping_count = int(data.get('ping_count', 5))
    m.interval = int(data.get('interval', 60))
    m.timeout_seconds = int(data.get('timeout_seconds', 2))
    m.original_ip_cdn_enabled = data.get('original_ip_cdn_enabled', False)
    m.backup_ip_cdn_enabled = data.get('backup_ip_cdn_enabled', False)
    
    m.schedule_enabled = data.get('schedule_enabled', False)
    m.schedule_hours = int(data.get('schedule_hours', 0))
    m.schedule_switch_ip = data.get('schedule_switch_ip', '')
    
    m.status = 'Normal'
    m.current_ip = m.original_ip
    
    db.session.add(m)
    db.session.commit()
    
    # Add check job
    scheduler.add_job(
        func=check_monitor_job,
        trigger='interval',
        seconds=m.interval,
        args=[m.id],
        id=m.id,
        replace_existing=True
    )
    
    # Add schedule job
    if m.schedule_enabled and m.schedule_hours > 0:
        scheduler.add_job(
            func=scheduled_switch_job,
            trigger='interval',
            hours=m.schedule_hours,
            args=[m.id],
            id=f"{m.id}_schedule",
            replace_existing=True
        )
    
    return jsonify({'code': 200, 'msg': 'success'})

@bp.route('/<id>', methods=['PUT'])
@require_auth
def update_monitor(id):
    m = Monitor.query.get(id)
    if not m:
        return jsonify({'code': 404, 'msg': 'Not found'}), 404
        
    data = request.json
    m.name = data.get('name', m.name)
    m.zone_id = data.get('zone_id', m.zone_id)
    m.subdomains = json.dumps(data.get('subdomains', []))
    m.check_type = data.get('check_type', m.check_type)
    m.check_target = data.get('check_target', m.check_target)
    m.original_ip = data.get('original_ip', m.original_ip)
    m.backup_ip = data.get('backup_ip', m.backup_ip)
    m.failure_threshold = int(data.get('failure_threshold', m.failure_threshold))
    m.success_threshold = int(data.get('success_threshold', m.success_threshold))
    m.ping_count = int(data.get('ping_count', m.ping_count))
    m.interval = int(data.get('interval', m.interval))
    m.timeout_seconds = int(data.get('timeout_seconds', m.timeout_seconds))
    m.original_ip_cdn_enabled = data.get('original_ip_cdn_enabled', m.original_ip_cdn_enabled)
    m.backup_ip_cdn_enabled = data.get('backup_ip_cdn_enabled', m.backup_ip_cdn_enabled)
    
    m.schedule_enabled = data.get('schedule_enabled', m.schedule_enabled)
    m.schedule_hours = int(data.get('schedule_hours', m.schedule_hours))
    m.schedule_switch_ip = data.get('schedule_switch_ip', m.schedule_switch_ip)
    
    db.session.commit()
    
    # Update check job
    if scheduler.get_job(id):
        scheduler.remove_job(id)
        
    scheduler.add_job(
        func=check_monitor_job,
        trigger='interval',
        seconds=m.interval,
        args=[m.id],
        id=m.id,
        replace_existing=True
    )
    
    # Update schedule job
    sched_job_id = f"{id}_schedule"
    if scheduler.get_job(sched_job_id):
        scheduler.remove_job(sched_job_id)
        
    if m.schedule_enabled and m.schedule_hours > 0:
        scheduler.add_job(
            func=scheduled_switch_job,
            trigger='interval',
            hours=m.schedule_hours,
            args=[m.id],
            id=sched_job_id,
            replace_existing=True
        )
    
    return jsonify({'code': 200, 'msg': 'success'})

@bp.route('/<id>', methods=['DELETE'])
@require_auth
def delete_monitor(id):
    m = Monitor.query.get(id)
    if not m:
        return jsonify({'code': 404, 'msg': 'Not found'}), 404
        
    db.session.delete(m)
    db.session.commit()
    
    if scheduler.get_job(id):
        scheduler.remove_job(id)
    
    sched_job_id = f"{id}_schedule"
    if scheduler.get_job(sched_job_id):
        scheduler.remove_job(sched_job_id)
        
    return jsonify({'code': 200, 'msg': 'success'})

@bp.route('/<id>/restore', methods=['POST'])
@require_auth
def restore_monitor(id):
    from app.core.engine import do_switch
    
    m = Monitor.query.get(id)
    if not m:
        return jsonify({'code': 404, 'msg': 'Not found'}), 404
        
    m.status = 'Normal'
    m.current_ip = m.original_ip
    m.fail_count = 0
    m.succ_count = 0
    db.session.commit()
    
    # Trigger switch to original
    try:
        do_switch(m, to_backup=False)
    except Exception as e:
        return jsonify({'code': 500, 'msg': str(e)}), 500
        
    return jsonify({'code': 200, 'msg': 'success'})
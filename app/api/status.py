from flask import Blueprint, jsonify, current_app
from app.models import Monitor, SwitchLog, IPDownEvent
from app.utils import require_auth
from datetime import datetime, date, timedelta
import time
import os

bp = Blueprint('status', __name__, url_prefix='/api/status')

START_TIME = time.time()

@bp.route('', methods=['GET'])
@require_auth
def get_status():
    # Monitors
    monitors = Monitor.query.all()
    mon_list = [m.to_dict() for m in monitors]
    
    # History
    history = SwitchLog.query.order_by(SwitchLog.timestamp.desc()).limit(50).all()
    hist_list = [h.to_dict() for h in history]
    
    # Offline Hot (Today)
    today_start = datetime.utcnow().replace(hour=0, minute=0, second=0, microsecond=0)
    events = IPDownEvent.query.filter(IPDownEvent.timestamp >= today_start).all()
    
    agg = {}
    for e in events:
        key = (e.monitor_id, e.ip, e.role)
        if key not in agg:
            agg[key] = {
                'monitor_id': e.monitor_id,
                'name': e.name,
                'ip': e.ip,
                'role': e.role,
                'count': 0,
                'last_at': 0
            }
        agg[key]['count'] += 1
        ts = int(e.timestamp.timestamp() * 1000)
        if ts > agg[key]['last_at']:
            agg[key]['last_at'] = ts
            
    offline_hot = [v for v in agg.values() if v['count'] >= 3]
    offline_hot.sort(key=lambda x: (x['count'], x['last_at']), reverse=True)
    
    # System
    uptime = int(time.time() - START_TIME)
    system = {
        'uptime_seconds': uptime,
        # 'mem_alloc': ... (requires psutil or complex logic, skipping for simple port)
    }
    
    return jsonify({
        'code': 200,
        'data': {
            'monitors': mon_list,
            'history': hist_list,
            'system': system,
            'offline_hot': offline_hot
        }
    })

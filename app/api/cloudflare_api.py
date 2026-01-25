from flask import Blueprint, request, jsonify
from app import db
from app.models import GlobalConfig, CloudflareAccount
from app.utils import require_auth
from app.core.services import CloudflareService
import json
import logging

bp = Blueprint('cloudflare_api', __name__, url_prefix='/api')

def get_cf_service():
    gc = GlobalConfig.query.get('cloudflare')
    if not gc:
        return CloudflareService()
    conf = json.loads(gc.value)
    return CloudflareService(
        api_token=conf.get('api_token'),
        api_key=conf.get('api_key'),
        email=conf.get('email')
    )

# --- Cloudflare Proxy ---

@bp.route('/zones', methods=['GET'])
@require_auth
def list_zones():
    svc = get_cf_service()
    zones = svc.list_zones()
    return jsonify({'code': 200, 'data': zones})

@bp.route('/zones/<zone_id>/records', methods=['GET'])
@require_auth
def list_records(zone_id):
    svc = get_cf_service()
    records = svc.list_records(zone_id)
    return jsonify({'code': 200, 'data': records})

@bp.route('/zones/<zone_id>/records', methods=['POST'])
@require_auth
def create_record(zone_id):
    svc = get_cf_service()
    data = request.json
    try:
        # CF python lib expects strict params, we pass data directly assuming it matches
        # Flask request.json is dict, CF lib expects kwargs or dict
        res = svc.create_record(zone_id, data)
        return jsonify({'code': 200, 'data': res})
    except Exception as e:
        return jsonify({'code': 500, 'msg': str(e)}), 500

@bp.route('/zones/<zone_id>/records/<record_id>', methods=['PUT'])
@require_auth
def update_record(zone_id, record_id):
    svc = get_cf_service()
    data = request.json
    try:
        res = svc.update_record(zone_id, record_id, data)
        return jsonify({'code': 200, 'data': res})
    except Exception as e:
        return jsonify({'code': 500, 'msg': str(e)}), 500

@bp.route('/zones/<zone_id>/records/<record_id>', methods=['DELETE'])
@require_auth
def delete_record(zone_id, record_id):
    svc = get_cf_service()
    try:
        svc.delete_record(zone_id, record_id)
        return jsonify({'code': 200, 'msg': 'success'})
    except Exception as e:
        return jsonify({'code': 500, 'msg': str(e)}), 500

# --- Accounts Management ---

@bp.route('/cloudflare-accounts', methods=['GET'])
@require_auth
def list_accounts():
    accounts = CloudflareAccount.query.order_by(CloudflareAccount.name).all()
    acc_list = [a.to_dict() for a in accounts]
    
    active_conf = GlobalConfig.query.get('active_account_id')
    active_id = active_conf.value if active_conf else None
    
    active_index = -1
    if active_id:
        for i, acc in enumerate(acc_list):
            if acc['id'] == active_id:
                active_index = i
                break
    
    # If no active ID found but we have accounts (and maybe one was auto-activated but not saved properly?), 
    # we can try to fallback? No, let's stick to explicit state.
    
    return jsonify({
        'code': 200, 
        'data': {
            'accounts': acc_list,
            'active_index': active_index
        }
    })

@bp.route('/cloudflare-accounts', methods=['POST'])
@require_auth
def add_account():
    import time
    data = request.json
    acc = CloudflareAccount()
    acc.id = data.get('id') or str(int(time.time()*1000))
    acc.name = data.get('name')
    acc.api_token = data.get('api_token')
    acc.api_key = data.get('api_key')
    acc.email = data.get('email')
    db.session.add(acc)
    db.session.commit()
    
    # Auto-activate if no active account exists
    active_conf = GlobalConfig.query.get('active_account_id')
    if not active_conf or not active_conf.value:
        activate_logic(acc.id)
        
    return jsonify({'code': 200, 'msg': 'success'})

@bp.route('/cloudflare-accounts/<id>', methods=['PUT'])
@require_auth
def update_account(id):
    acc = CloudflareAccount.query.get(id)
    if not acc: return jsonify({'code': 404}), 404
    data = request.json
    acc.name = data.get('name')
    acc.api_token = data.get('api_token')
    acc.api_key = data.get('api_key')
    acc.email = data.get('email')
    db.session.commit()
    return jsonify({'code': 200, 'msg': 'success'})

@bp.route('/cloudflare-accounts/<id>', methods=['DELETE'])
@require_auth
def delete_account(id):
    acc = CloudflareAccount.query.get(id)
    if acc:
        db.session.delete(acc)
        db.session.commit()
    return jsonify({'code': 200, 'msg': 'success'})

def activate_logic(id):
    acc = CloudflareAccount.query.get(id)
    if not acc: return
    
    cf_conf = {
        'api_token': acc.api_token,
        'api_key': acc.api_key,
        'email': acc.email
    }
    
    gc = GlobalConfig.query.get('cloudflare')
    if not gc:
        gc = GlobalConfig(key='cloudflare')
        db.session.add(gc)
    gc.value = json.dumps(cf_conf)
    
    active = GlobalConfig.query.get('active_account_id')
    if not active:
        active = GlobalConfig(key='active_account_id')
        db.session.add(active)
    active.value = id
    db.session.commit()

@bp.route('/cloudflare-accounts/<id>/activate', methods=['POST'])
@require_auth
def activate_account(id):
    acc = CloudflareAccount.query.get(id)
    if not acc: return jsonify({'code': 404}), 404
    
    activate_logic(id)
    return jsonify({'code': 200, 'msg': 'success'})

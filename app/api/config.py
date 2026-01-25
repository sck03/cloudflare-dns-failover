from flask import Blueprint, request, jsonify
from app import db
from app.models import GlobalConfig
from app.utils import require_auth
import json

bp = Blueprint('config', __name__, url_prefix='/api/config')

@bp.route('', methods=['GET'])
@require_auth
def get_global_config():
    keys = ['cloudflare', 'dingtalk', 'email', 'telegram']
    res = {}
    for k in keys:
        gc = GlobalConfig.query.get(k)
        if gc:
            try:
                res[k] = json.loads(gc.value)
            except:
                res[k] = {}
        else:
            res[k] = {}
            
    return jsonify({'code': 200, 'data': res})

@bp.route('', methods=['POST'])
@require_auth
def update_global_config():
    data = request.json
    # Expects {cloudflare: {}, dingtalk: {}, ...}
    
    keys = ['cloudflare', 'dingtalk', 'email', 'telegram']
    for k in keys:
        if k in data:
            val = json.dumps(data[k])
            gc = GlobalConfig.query.get(k)
            if not gc:
                gc = GlobalConfig(key=k)
                db.session.add(gc)
            gc.value = val
            
    db.session.commit()
    return jsonify({'code': 200, 'msg': 'success'})

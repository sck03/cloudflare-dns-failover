from flask import Blueprint, request, jsonify, make_response
from app import db
from app.models import GlobalConfig
from app.utils import get_auth_token
import json

bp = Blueprint('auth', __name__, url_prefix='/api/auth')

@bp.route('/status', methods=['GET'])
def auth_status():
    token = get_auth_token()
    has_token = token is not None
    return jsonify({
        'code': 200, 
        'data': {
            'has_token': has_token,
            'need_setup': not has_token
        }
    })

@bp.route('/check', methods=['GET'])
def check_auth():
    token = get_auth_token()
    if not token:
        return jsonify({'code': 200, 'data': {'authenticated': True, 'need_setup': True}})
    
    cookie_token = request.cookies.get('auth_token')
    authenticated = cookie_token == token
    
    return jsonify({
        'code': 200,
        'data': {
            'authenticated': authenticated,
            'need_setup': False
        }
    })

@bp.route('/login', methods=['POST'])
def login():
    data = request.json
    req_token = data.get('token')
    if not req_token:
        return jsonify({'code': 400, 'msg': 'Token required'}), 400
        
    stored_token = get_auth_token()
    
    if not stored_token:
        # First time setup
        conf = GlobalConfig(key='auth_token', value=json.dumps(req_token))
        db.session.add(conf)
        db.session.commit()
    else:
        if req_token != stored_token:
            return jsonify({'code': 401, 'msg': 'Invalid token'}), 401
            
    resp = make_response(jsonify({'code': 200, 'msg': 'Login success'}))
    resp.set_cookie('auth_token', req_token, max_age=86400, httponly=True)
    return resp

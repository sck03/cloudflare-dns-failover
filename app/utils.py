from functools import wraps
from flask import request, jsonify, current_app
from app.models import GlobalConfig
import json

def get_auth_token():
    conf = GlobalConfig.query.get('auth_token')
    if conf:
        # Assuming value is just the string, or JSON string?
        # Let's assume JSON string to be consistent with other config
        try:
            return json.loads(conf.value)
        except:
            return conf.value
    return None

def require_auth(f):
    @wraps(f)
    def decorated(*args, **kwargs):
        token = get_auth_token()
        if not token:
            # First time setup, allow
            return f(*args, **kwargs)
            
        cookie_token = request.cookies.get('auth_token')
        if not cookie_token or cookie_token != token:
            return jsonify({'code': 401, 'msg': 'Unauthorized'}), 401
            
        return f(*args, **kwargs)
    return decorated

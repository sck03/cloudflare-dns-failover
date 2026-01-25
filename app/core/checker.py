import requests
from ping3 import ping
import logging

def check_ping(target, count=5, timeout=2):
    """
    Returns True if packet loss < 60% (as per original Go logic)
    """
    if not target:
        return False
        
    success_count = 0
    for _ in range(count):
        try:
            # ping returns delay in seconds, or None/False on error
            delay = ping(target, timeout=timeout, unit='s')
            if delay is not None:
                success_count += 1
        except Exception as e:
            logging.error(f"Ping error for {target}: {e}")
    
    # Calculate packet loss
    loss_rate = (count - success_count) / count
    return loss_rate < 0.6

def check_http(target, timeout=10):
    """
    Returns True if status code 200-399
    """
    if not target:
        return False
        
    try:
        if not target.startswith('http'):
            target = 'http://' + target
            
        resp = requests.get(target, timeout=timeout)
        return 200 <= resp.status_code < 400
    except Exception as e:
        logging.error(f"HTTP check error for {target}: {e}")
        return False

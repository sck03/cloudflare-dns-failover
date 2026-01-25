from flask import Flask
from flask_sqlalchemy import SQLAlchemy
from apscheduler.schedulers.background import BackgroundScheduler
from config import Config
import os

db = SQLAlchemy()
# We use standard BackgroundScheduler but we might want to attach it to app if using flask-apscheduler
# For simplicity, we can use a global scheduler instance or manage it within the engine.
# Let's use a global one here for easy access, but initialized in create_app.
scheduler = BackgroundScheduler()

def create_app(config_class=Config):
    app = Flask(__name__, static_folder='../static', static_url_path='')
    app.config.from_object(config_class)

    # Ensure instance folder exists
    try:
        os.makedirs(app.instance_path)
    except OSError:
        pass

    db.init_app(app)

    with app.app_context():
        # Import models to ensure they are registered with SQLAlchemy
        from . import models
        db.create_all()
        
        # Initialize and start scheduler
        # In a real production deployment with uWSGI/Gunicorn multiple workers, 
        # this needs a lock or dedicated worker. For this project, single process is assumed.
        if not scheduler.running:
            scheduler.start()
            
        # Import and register blueprints
        from .api import auth, monitor, cloudflare_api, config as config_bp, status
        app.register_blueprint(auth.bp)
        app.register_blueprint(monitor.bp)
        app.register_blueprint(cloudflare_api.bp)
        app.register_blueprint(config_bp.bp)
        app.register_blueprint(status.bp)
        
        # Serve frontend for root path
        @app.route('/')
        def index():
            return app.send_static_file('index.html')

    return app

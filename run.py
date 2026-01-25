from app import create_app
from app.core.engine import init_engine

app = create_app()
init_engine(app)

if __name__ == '__main__':
    # Determine port from env or default
    port = 8081
    app.run(host='0.0.0.0', port=port, debug=True, use_reloader=False) 
    # use_reloader=False prevents double scheduler start in debug mode

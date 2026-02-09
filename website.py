from flask import Flask, request, render_template_string
import datetime
import json

app = Flask(__name__)
alerts = []

# HTML Layout with a dark 'SOC' theme
HTML_TEMPLATE = '''
<!DOCTYPE html>
<html>
<head>
    <title>CCDC Mission Control - Alerts</title>
    <meta http-equiv="refresh" content="5"> 
    <style>
        body { font-family: 'Courier New', Courier, monospace; background: #0d0d0d; color: #00ff00; padding: 20px; }
        h1 { border-bottom: 2px solid #00ff00; padding-bottom: 10px; }
        .alert-card { border: 1px solid #00ff00; margin-bottom: 15px; padding: 15px; background: #1a1a1a; border-radius: 5px; }
        .timestamp { color: #ff8800; font-weight: bold; }
        .source { color: #00bcff; }
        pre { background: #000; padding: 10px; overflow-x: auto; color: #00ff00; border: 1px dashed #333; }
        .label { font-weight: bold; text-transform: uppercase; font-size: 0.8em; color: #888; }
    </style>
</head>
<body>
    <h1>⚠️ LIVE SECURITY ALERTS</h1>
    {% if not alerts %}
        <p>No alerts received yet. Monitoring network...</p>
    {% endif %}
    {% for alert in alerts[::-1] %}
    <div class="alert-card">
        <span class="label">Time:</span> <span class="timestamp">{{ alert.time }}</span> | 
        <span class="label">Alert Name:</span> <span class="source">{{ alert.search_name }}</span>
        <br><br>
        <span class="label">Payload Data:</span>
        <pre>{{ alert.full_data | tojson(indent=2) }}</pre>
    </div>
    {% endfor %}
</body>
</html>
'''

@app.route('/webhook', methods=['POST'])
def webhook():
    raw_data = request.json
    
    # Splunk webhooks usually send the alert name in 'search_name'
    alert_entry = {
        'time': datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S"),
        'search_name': raw_data.get('search_name', 'Unknown Alert'),
        'full_data': raw_data
    }
    
    alerts.append(alert_entry)
    # Keep only the last 50 alerts to save memory
    if len(alerts) > 50:
        alerts.pop(0)
        
    return 'Alert Received', 200

@app.route('/')
def index():
    return render_template_string(HTML_TEMPLATE, alerts=alerts)

if __name__ == '__main__':
    # Listens on all interfaces at port 5000
    app.run(host='0.0.0.0', port=5000)

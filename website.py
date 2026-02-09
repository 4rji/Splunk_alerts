from flask import Flask, request, render_template_string
import datetime
import uuid

app = Flask(__name__)
alerts = []

HTML_TEMPLATE = '''
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>CCDC Tactical Alert Board</title>
    <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css" rel="stylesheet">
    <meta http-equiv="refresh" content="10">
    <style>
        body { background-color: #0b0e14; color: #e0e6ed; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; }
        .container { max-width: 900px; margin-top: 30px; }
        .card { background-color: #1a1f29; border: 1px solid #30363d; margin-bottom: 10px; }
        .card-header { cursor: pointer; background-color: #21262d; border-bottom: 1px solid #30363d; }
        .card-header:hover { background-color: #30363d; }
        .alert-name { color: #58a6ff; font-weight: bold; font-size: 1.1rem; }
        .timestamp { color: #8b949e; font-size: 0.9rem; float: right; }
        pre { background-color: #0d1117; color: #7ee787; padding: 15px; border-radius: 5px; border: 1px solid #30363d; }
        .badge-new { background-color: #238636; margin-right: 10px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="d-flex justify-content-between align-items-center mb-4 border-bottom pb-2">
            <h1>🛡️ CCDC Monitoring Station</h1>
            <span class="badge bg-secondary">Auto-refresh: 10s</span>
        </div>

        {% if not alerts %}
            <div class="text-center mt-5">
                <p class="text-muted">Waiting for incoming Splunk webhooks...</p>
                <div class="spinner-border text-primary" role="status"></div>
            </div>
        {% endif %}

        <div class="accordion" id="alertAccordion">
            {% for alert in alerts[::-1] %}
            <div class="card">
                <div class="card-header" id="heading{{ alert.id }}" data-bs-toggle="collapse" data-bs-target="#collapse{{ alert.id }}">
                    <span class="badge badge-new">NEW</span>
                    <span class="alert-name">{{ alert.search_name }}</span>
                    <span class="timestamp">{{ alert.time }}</span>
                </div>

                <div id="collapse{{ alert.id }}" class="collapse" data-bs-parent="#alertAccordion">
                    <div class="card-body">
                        <p><strong>Raw Payload Analysis:</strong></p>
                        <pre><code>{{ alert.full_data | tojson(indent=2) }}</code></pre>
                    </div>
                </div>
            </div>
            {% endfor %}
        </div>
    </div>

    <script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/js/bootstrap.bundle.min.js"></script>
</body>
</html>
'''

@app.route('/webhook', methods=['POST'])
def webhook():
    raw_data = request.json
    alert_entry = {
        'id': str(uuid.uuid4())[:8],  # Short unique ID for the tabs/accordion
        'time': datetime.datetime.now().strftime("%H:%M:%S"),
        'search_name': raw_data.get('search_name', 'System Alert'),
        'full_data': raw_data
    }
    alerts.append(alert_entry)
    if len(alerts) > 30: alerts.pop(0)
    return 'OK', 200

@app.route('/')
def index():
    return render_template_string(HTML_TEMPLATE, alerts=alerts)

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)

# 🛡️ CCDC Real-Time Alert Dashboard

A lightweight, local SOC dashboard designed for **CCDC (Collegiate Cyber Defense Competition)**. This tool receives Splunk Webhooks and displays security alerts in a clean, filterable web interface, allowing teams to monitor attacks without external internet access.

## 🚀 Setup Guide

### Step 1: Create the Splunk Search

Run a search in Splunk to identify the specific activity you want to monitor. For example, to track failed SSH logins:

```splunk
index=linux "sshd-session" "failed" OR "invalid"
```

### Step 2: Save as Alert

Once your search is working, save it as an alert to automate the notification process.

1. Click on **Save As** in the top-right corner.
2. Select **Alert**.

### Step 3: Configure Webhook Action

In the Alert settings, you must tell Splunk to send the data to your dashboard's IP address.

1. Under **Trigger Actions**, click **Add Action**.
2. Select **Webhook**.
3. Enter your dashboard URL: `http://<YOUR_IP>:5000/webhook`

### Step 4: Launch the Dashboard

On your monitoring station, ensure you have **Flask** installed and run the server script.

```bash
# Install dependencies
pip install flask

# Start the listener
python3 website.py
```

Now, open your browser and navigate to `http://localhost:5000` to see your live attack feed.

---

## 🛠️ Requirements

* **Splunk Enterprise** (Local or VM)
* **Python 3.x**
* **Flask**

## 💡 Competition Tip

Remember to enable **Throttling** in the Splunk alert settings to prevent the Red Team from flooding your dashboard with thousands of messages during a brute-force attack!

---

## 🔧 Troubleshooting

### Webhook Not Receiving Data

**Problem**: Alerts are firing in Splunk, but nothing appears on the dashboard.

**Solutions**:
1. **Check the IP address**: Ensure `<YOUR_IP>` in the webhook URL matches your dashboard server's IP
2. **Verify the port**: Confirm port `5000` is not blocked by a firewall
3. **Test connectivity**: From the Splunk server, run:
   ```bash
   curl -X POST http://<YOUR_IP>:5000/webhook -H "Content-Type: application/json" -d '{"test":"data"}'
   ```
4. **Check Flask logs**: Look for incoming POST requests in the terminal running `website.py`

### Dashboard Won't Start

**Problem**: `python3 website.py` fails or shows errors.

**Solutions**:
1. **Install Flask**: Run `pip install flask` or `pip3 install flask`
2. **Check Python version**: Ensure you're using Python 3.x with `python3 --version`
3. **Port already in use**: If port 5000 is occupied, modify the port in `website.py`

### Alerts Not Triggering in Splunk

**Problem**: The search works manually, but the alert doesn't fire.

**Solutions**:
1. **Check alert schedule**: Ensure the alert is set to run at appropriate intervals
2. **Verify trigger conditions**: Confirm the alert trigger condition matches your search results
3. **Review alert permissions**: Ensure your Splunk user has permission to create and run alerts

### Connection Refused During Competition

**Problem**: Dashboard works during testing but fails on February 21st.

**Solutions**:
1. **Verify network isolation**: Ensure both Splunk and the dashboard are on the same local network
2. **Check firewall rules**: Red Team activity might trigger defensive firewall rules blocking port 5000
3. **Use static IP**: Avoid DHCP issues by assigning a static IP to your dashboard server
4. **Test before competition**: Run a full end-to-end test the night before

### Too Many Alerts (Dashboard Overload)

**Problem**: Brute-force attacks flood the dashboard with thousands of alerts.

**Solutions**:
1. **Enable throttling**: In Splunk alert settings, set throttling to suppress duplicate alerts (e.g., once every 5 minutes)
2. **Adjust search**: Refine your search to be more specific and reduce noise
3. **Implement rate limiting**: Modify `website.py` to limit the number of alerts displayed or stored

---

## 📝 Quick Reference

| Component | Location | Purpose |
|-----------|----------|---------|
| Splunk Alert | Splunk Web UI | Triggers webhook on security events |
| Dashboard Server | `website.py` | Receives and displays alerts |
| Webhook Endpoint | `http://<YOUR_IP>:5000/webhook` | Receives POST requests from Splunk |
| Web Interface | `http://localhost:5000` | View live alerts |

---

**Good luck at CCDC 2026! 🎯**

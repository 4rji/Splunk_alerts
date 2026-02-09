# 🛡️ CCDC Real-Time Alert Dashboard

A lightweight, local SOC dashboard designed for **CCDC (Collegiate Cyber Defense Competition)**. This tool receives Splunk Webhooks and displays security alerts in a clean, filterable web interface, allowing teams to monitor attacks without external internet access.

![Dashboard Interface](dashboard.webp)  

## 🚀 Setup Guide

### Step 1: Create the Splunk Search

Run a search in Splunk to identify the specific activity you want to monitor. For example, to track failed SSH logins:

```splunk
index=linux "sshd-session" "failed" OR "invalid"
```

---

### Step 2: Save as Alert

Once your search is working, save it as an alert to automate the notification process.

1. Click on **Save As** in the top-right corner.
2. Select **Alert**.

![Save as Alert](alerts1.webp)

---

### Step 3: Configure Webhook Action

In the Alert settings, you must tell Splunk to send the data to your dashboard's IP address.

1. Under **Trigger Actions**, click **Add Action**.
2. Select **Webhook**.
3. Enter your dashboard URL: `http://<YOUR_IP>:5000/webhook`

![Save as Alert](alerts2.webp)

---

### Step 4: Launch the Dashboard

On your monitoring station, ensure you have **Flask** installed and run the server script.

```bash
# Install dependencies
pip install flask

# Start the listener
python3 website.py
```

Now, open your browser and navigate to `http://localhost:5000` to see your live attack feed.

![Save as Alert](web.webp)

---

## 🛠️ Requirements

* **Splunk Enterprise** (Local or VM)
* **Python 3.x**
* **Flask**

## 💡 Competition Tip

Remember to enable **Throttling** in the Splunk alert settings to prevent the Red Team from flooding your dashboard with thousands of messages during a brute-force attack!

---

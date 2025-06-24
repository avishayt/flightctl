# Alerts and Monitoring

FlightControl provides a comprehensive alerting system that monitors your edge devices and fleets, automatically detecting and notifying you of issues that require attention. The alerts system helps you maintain visibility into device health, connectivity, and application status across your entire edge infrastructure.

## Overview

The FlightControl alerts system continuously monitors device events and automatically generates alerts when issues are detected. These alerts are integrated with Prometheus Alertmanager, providing powerful notification and management capabilities.

### What Gets Monitored

FlightControl automatically monitors and alerts on:

- **Device Connectivity**: Connection/disconnection events
- **Resource Utilization**: CPU, memory, and disk usage warnings and critical levels
- **Application Health**: Application deployment status and health checks
- **Device Lifecycle**: Device creation, updates, and removal
- **Fleet Operations**: Fleet management and rollout status

## Alert Types

### Device Health Alerts

| Alert Name | Description | Trigger Condition |
|------------|-------------|------------------|
| `DeviceDisconnected` | Device has lost connection to FlightControl | Device hasn't reported status for configured interval |
| `DeviceCPUWarning` | Device CPU usage is elevated | CPU utilization above warning threshold |
| `DeviceCPUCritical` | Device CPU usage is critically high | CPU utilization above critical threshold |
| `DeviceMemoryWarning` | Device memory usage is elevated | Memory utilization above warning threshold |
| `DeviceMemoryCritical` | Device memory usage is critically high | Memory utilization above critical threshold |
| `DeviceDiskWarning` | Device disk usage is elevated | Disk utilization above warning threshold |
| `DeviceDiskCritical` | Device disk usage is critically high | Disk utilization above critical threshold |

### Application Alerts

| Alert Name | Description | Trigger Condition |
|------------|-------------|------------------|
| `DeviceApplicationError` | Application failed to deploy or is unhealthy | Application deployment failed or health check failed |

### Alert Resolution

Alerts are automatically resolved when the underlying condition is fixed:

- **DeviceDisconnected** → Resolved when device reconnects (`DeviceConnected`)
- **Resource alerts** → Resolved when usage returns to normal levels (e.g., `DeviceCPUNormal`)
- **Application alerts** → Resolved when application becomes healthy (`DeviceApplicationHealthy`)

## Accessing Alerts

### Prerequisites

To access alerts, you need:

1. Valid FlightControl authentication token
2. Permission to view alerts resources
3. Access to the FlightControl alertmanager proxy

### Using the Alertmanager Web UI

FlightControl provides an authenticated proxy to Alertmanager that integrates with your existing FlightControl authentication.

1. **Get your authentication token** (varies by auth method):
   - **OIDC**: Obtain token from your identity provider
   - **OpenShift**: Use `oc whoami -t` command
   - **AAP**: Use your AAP Gateway token

2. **Access the proxy**:
   ```bash
   # Using curl with bearer token
   curl -H "Authorization: Bearer <your-token>" \
        http://alertmanager-proxy:8443/api/v2/alerts
   ```

3. **View alerts in browser**: Navigate to the Alertmanager proxy URL and authenticate

### Using the API

Query active alerts programmatically:

```bash
# List all active alerts
curl -H "Authorization: Bearer <your-token>" \
     http://alertmanager-proxy:8443/api/v2/alerts

# Filter alerts by device
curl -H "Authorization: Bearer <your-token>" \
     "http://alertmanager-proxy:8443/api/v2/alerts?filter[resource]=my-device"

# Filter alerts by type
curl -H "Authorization: Bearer <your-token>" \
     "http://alertmanager-proxy:8443/api/v2/alerts?filter[alertname]=DeviceDisconnected"
```

## Configuration

### Alert Polling Interval

Configure how frequently FlightControl checks for new events to generate alerts:

```yaml
# In your FlightControl configuration
service:
  alertPollingInterval: "30s"  # Check for new events every 30 seconds
```

### Alertmanager Integration

FlightControl automatically connects to Alertmanager when deployed. The connection is configured via:

```yaml
# In your FlightControl configuration
alertmanager:
  hostname: "flightctl-alertmanager"
  port: 9093
```

### Enabling/Disabling Components

Control which alert components are deployed:

```yaml
# In Helm values.yaml
alertExporter:
  enabled: true  # Set to false to disable alert generation

alertmanagerProxy:
  enabled: true  # Set to false to disable authenticated access

alertmanager:
  enabled: true  # Set to false to disable Alertmanager entirely
```

## Notification Setup

FlightControl uses Prometheus Alertmanager for notifications. Configure notification channels in your Alertmanager configuration:

### Email Notifications

```yaml
# alertmanager.yml
global:
  smtp_smarthost: 'smtp.example.com:587'
  smtp_from: 'flightctl@example.com'

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'email-notifications'

receivers:
- name: 'email-notifications'
  email_configs:
  - to: 'admin@example.com'
    subject: 'FlightControl Alert: {{ .GroupLabels.alertname }}'
    body: |
      {{ range .Alerts }}
      Alert: {{ .Annotations.summary }}
      Device: {{ .Labels.resource }}
      {{ end }}
```

### Slack Notifications

```yaml
# alertmanager.yml
receivers:
- name: 'slack-notifications'
  slack_configs:
  - api_url: 'YOUR_SLACK_WEBHOOK_URL'
    channel: '#flightctl-alerts'
    title: 'FlightControl Alert'
    text: |
      {{ range .Alerts }}
      *{{ .Labels.alertname }}* on device {{ .Labels.resource }}
      {{ end }}
```

### Webhook Notifications

```yaml
# alertmanager.yml
receivers:
- name: 'webhook-notifications'
  webhook_configs:
  - url: 'http://your-webhook-endpoint.com/alerts'
    send_resolved: true
```

## Alert Labels and Filtering

Every FlightControl alert includes these labels:

- `alertname`: The type of alert (e.g., "DeviceDisconnected")
- `resource`: The name of the affected device or fleet
- `org_id`: The organization ID (for multi-tenant setups)

Use these labels to create targeted notification rules and filters:

```yaml
# Route critical CPU alerts to on-call team
routes:
- match:
    alertname: DeviceCPUCritical
  receiver: 'oncall-team'
  
# Route disconnection alerts to monitoring team
- match:
    alertname: DeviceDisconnected
  receiver: 'monitoring-team'
```

## Troubleshooting

### No Alerts Appearing

1. **Check alert exporter status**:
   ```bash
   # For Kubernetes
   kubectl logs deployment/flightctl-alert-exporter
   
   # For Quadlets
   sudo journalctl -u flightctl-alert-exporter.service
   ```

2. **Verify Alertmanager connectivity**:
   ```bash
   # Check if Alertmanager is reachable
   curl http://alertmanager:9093/api/v2/alerts
   ```

3. **Check authentication**:
   ```bash
   # Verify your token works
   curl -H "Authorization: Bearer <your-token>" \
        http://alertmanager-proxy:8443/api/v2/status
   ```

### Alerts Not Resolving

1. **Check if resolution events are being generated**:
   - Verify devices are reconnecting
   - Confirm resource usage has returned to normal
   - Check application health status

2. **Review event logs**:
   ```bash
   # Check recent events
   flightctl get events --limit 50
   ```

### Missing Alert Notifications

1. **Verify Alertmanager configuration**:
   ```bash
   # Check Alertmanager config
   curl http://alertmanager:9093/api/v2/status
   ```

2. **Test notification channels**:
   - Send test alerts to verify email/Slack/webhook configuration
   - Check Alertmanager logs for delivery errors

3. **Review routing rules**:
   - Ensure alert labels match your routing configuration
   - Verify receiver configurations are correct

### Performance Issues

1. **Adjust polling interval** if system is under heavy load:
   ```yaml
   service:
     alertPollingInterval: "60s"  # Reduce frequency
   ```

2. **Monitor alert exporter resource usage**:
   ```bash
   # Check memory and CPU usage
   kubectl top pod -l flightctl.service=flightctl-alert-exporter
   ```

## Best Practices

### Alert Routing Strategy

1. **Prioritize by severity**:
   - Route critical alerts (CPU/Memory critical, disconnections) to immediate notification channels
   - Route warning alerts to monitoring dashboards or delayed notifications

2. **Group by device or fleet**:
   - Avoid alert storms by grouping related alerts
   - Use appropriate group intervals to batch notifications

### Retention and Cleanup

1. **Configure alert retention**:
   ```yaml
   # In Alertmanager configuration
   global:
     resolve_timeout: 5m  # Auto-resolve alerts after 5 minutes of no updates
   ```

2. **Regular maintenance**:
   - Monitor alert volume and adjust thresholds if needed
   - Review and update notification channels regularly

### Integration with Monitoring

1. **Combine with metrics**: Use alerts alongside FlightControl metrics for comprehensive monitoring
2. **Dashboard integration**: Display alert status in monitoring dashboards
3. **Incident management**: Integrate alerts with your incident response tools

## Examples

### View All Active Alerts

```bash
curl -H "Authorization: Bearer <token>" \
     http://alertmanager-proxy:8443/api/v2/alerts | jq '.'
```

### Check Specific Device Alerts

```bash
curl -H "Authorization: Bearer <token>" \
     "http://alertmanager-proxy:8443/api/v2/alerts?filter[resource]=my-edge-device" | jq '.'
```

### Monitor Alert Count

```bash
# Count active alerts
curl -s -H "Authorization: Bearer <token>" \
     http://alertmanager-proxy:8443/api/v2/alerts | jq 'length'
```

This alerts system provides comprehensive monitoring for your FlightControl edge infrastructure, helping you maintain visibility and quickly respond to issues across your device fleet. 
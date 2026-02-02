#include "mqtt_client.h"
#include "task_monitor.h"
#include "wifi_manager.h"
#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>

LOG_MODULE_REGISTER(main);

#define WIFI_SSID "<YOUR_WIFI_SSID>"
#define WIFI_PSK "<YOUR_WIFI_PSK>"
#define PROPLET_ID "<YOUR_PROPLET_ID>"
#define DOMAIN_ID "<YOUR_DOMAIN_ID>"
#define CHANNEL_ID "<YOUR_CHANNEL_ID>"
#define PROPLET_NAMESPACE "embedded"

#ifndef PROPLET_LIVELINESS_INTERVAL_MS
#define PROPLET_LIVELINESS_INTERVAL_MS 10000
#endif

#ifndef PROPLET_METRICS_INTERVAL_MS
#define PROPLET_METRICS_INTERVAL_MS 30000
#endif

#ifndef PROPLET_TASK_METRICS_INTERVAL_MS
#define PROPLET_TASK_METRICS_INTERVAL_MS 10000
#endif

const char *domain_id = DOMAIN_ID;
const char *channel_id = CHANNEL_ID;

int main(void)
{
  LOG_INF("Starting Proplet...");

  wifi_manager_init();
  wifi_manager_connect(WIFI_SSID, WIFI_PSK);

  if (task_monitor_init() != 0) {
    LOG_WRN("Task monitor init failed, metrics may be unavailable");
  }

  if (mqtt_client_connect(domain_id, PROPLET_ID, channel_id) != 0) {
    LOG_ERR("MQTT connect failed");
    return -1;
  }

  if (subscribe(domain_id, channel_id) != 0) {
    LOG_ERR("MQTT subscribe failed");
    return -1;
  }

  if (publish_discovery(domain_id, PROPLET_ID, channel_id) != 0) {
    LOG_ERR("MQTT discovery publish failed");
    return -1;
  }

  for (int i = 0; i < 20; i++) {
    mqtt_client_process();
    k_sleep(K_MSEC(100));
  }

  int64_t next_alive = k_uptime_get() + PROPLET_LIVELINESS_INTERVAL_MS;
  int64_t next_metrics = k_uptime_get() + PROPLET_METRICS_INTERVAL_MS;
  int64_t next_task_metrics = k_uptime_get() + PROPLET_TASK_METRICS_INTERVAL_MS;

  while (1) {
    mqtt_client_process();

    int64_t now = k_uptime_get();

    if (now >= next_alive) {
      publish_alive_message(domain_id, channel_id);
      next_alive = now + PROPLET_LIVELINESS_INTERVAL_MS;
    }

    if (now >= next_metrics) {
      publish_metrics_message(domain_id, channel_id, PROPLET_ID, PROPLET_NAMESPACE);
      next_metrics = now + PROPLET_METRICS_INTERVAL_MS;
    }

    if (now >= next_task_metrics) {
      publish_active_task_metrics(domain_id, channel_id, PROPLET_ID);
      next_task_metrics = now + PROPLET_TASK_METRICS_INTERVAL_MS;
    }

    k_sleep(K_MSEC(200));
  }
}

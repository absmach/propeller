#include "mqtt_client.h"
#include "wifi_manager.h"
#include <zephyr/kernel.h>
#include <zephyr/logging/log.h>

LOG_MODULE_REGISTER(main);

#define WIFI_SSID "<YOUR_WIFI_SSID>"
#define WIFI_PSK "<YOUR_WIFI_PSK>"
#define PROPLET_ID "<YOUR_PROPLET_ID>"
#define PROPLET_PASSWORD "<YOUR_PROPLET_PASSWORD>"
#define DOMAIN_ID "<YOUR_DOMAIN_ID>"
#define CHANNEL_ID "<YOUR_CHANNEL_ID>"

const char *domain_id = DOMAIN_ID;
const char *channel_id = CHANNEL_ID;

int main(void)
{
  LOG_INF("Starting Proplet...");

  wifi_manager_init();

  wifi_manager_connect(WIFI_SSID, WIFI_PSK);

  mqtt_client_connect(DOMAIN_ID, PROPLET_ID, CHANNEL_ID);

  publish_discovery(DOMAIN_ID, PROPLET_ID, CHANNEL_ID);

  subscribe(DOMAIN_ID, CHANNEL_ID);

  while (1)
  {
    mqtt_client_process();

    if (mqtt_connected)
    {
      publish_alive_message(DOMAIN_ID, CHANNEL_ID);
    }
    else
    {
      LOG_WRN("MQTT client is not connected");
    }

    k_sleep(K_SECONDS(5));
  }
}

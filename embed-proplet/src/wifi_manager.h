#ifndef WIFI_MANAGER_H
#define WIFI_MANAGER_H

#include <zephyr/net/wifi_mgmt.h>
#include <zephyr/kernel.h>

void wifi_manager_init(void);
int wifi_manager_connect(const char *ssid, const char *psk);
int wifi_manager_enable_ap(const char *ssid, const char *psk, const char *ip_address, const char *netmask);
bool wifi_manager_wait_for_connection(k_timeout_t timeout); // Wait for Wi-Fi connection

#endif

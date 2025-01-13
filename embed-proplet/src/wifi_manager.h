#ifndef WIFI_MANAGER_H
#define WIFI_MANAGER_H

#include <zephyr/net/wifi_mgmt.h>

void wifi_manager_init(void);
int wifi_manager_connect(const char *ssid, const char *psk);
int wifi_manager_enable_ap(const char *ssid, const char *psk, const char *ip_address, const char *netmask);
bool wifi_manager_is_connected(void);

#endif

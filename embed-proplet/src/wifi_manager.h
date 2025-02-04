#ifndef WIFI_MANAGER_H
#define WIFI_MANAGER_H

#include <zephyr/kernel.h>
#include <zephyr/net/wifi_mgmt.h>

/**
 * @brief Initialize the Wi-Fi manager and set up event callbacks.
 */
void wifi_manager_init(void);

/**
 * @brief Connect to a Wi-Fi network with the given SSID and PSK.
 *
 * @param ssid The SSID of the Wi-Fi network.
 * @param psk The pre-shared key (PSK) for the Wi-Fi network.
 * @return 0 on successful connection, or a negative error code on failure.
 */
int wifi_manager_connect(const char *ssid, const char *psk);

/**
 * @brief Enable Access Point (AP) mode with the specified settings.
 *
 * @param ssid The SSID of the AP.
 * @param psk The pre-shared key (PSK) for the AP (optional).
 * @param ip_address The static IP address for the AP.
 * @param netmask The netmask for the AP's network.
 * @return 0 on success, or a negative error code on failure.
 */
int wifi_manager_enable_ap(const char *ssid, const char *psk,
                           const char *ip_address, const char *netmask);

#endif /* WIFI_MANAGER_H */

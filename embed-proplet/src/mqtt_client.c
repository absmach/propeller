// #include "mqtt_client.h"
// #include <zephyr/logging/log.h>
// #include <zephyr/net/socket.h>
// #include <zephyr/kernel.h>

// LOG_MODULE_REGISTER(mqtt_client);

// #define RX_BUFFER_SIZE 256
// #define TX_BUFFER_SIZE 256

// #define MQTT_BROKER_HOSTNAME "192.168.1.100" /* Replace with your broker's IP */
// #define MQTT_BROKER_PORT 1883

// #define DISCOVERY_TOPIC_TEMPLATE "channels/%s/messages/control/proplet/create"
// #define START_TOPIC_TEMPLATE "channels/%s/messages/control/manager/start"
// #define STOP_TOPIC_TEMPLATE "channels/%s/messages/control/manager/stop"

// #define CLIENT_ID "proplet-esp32s3"

// /* Buffers for MQTT client */
// static uint8_t rx_buffer[RX_BUFFER_SIZE];
// static uint8_t tx_buffer[TX_BUFFER_SIZE];

// /* MQTT client context */
// static struct mqtt_client client_ctx;
// static struct sockaddr_storage broker_addr;

// /* Flags to indicate connection status */
// static bool mqtt_connected = false;

// static void mqtt_event_handler(struct mqtt_client *client, const struct mqtt_evt *evt)
// {
//     switch (evt->type) {
//     case MQTT_EVT_CONNACK:
//         if (evt->result == 0) {
//             mqtt_connected = true;
//             LOG_INF("Connected to MQTT broker");
//         } else {
//             LOG_ERR("Connection failed, result: %d", evt->result);
//         }
//         break;

//     case MQTT_EVT_DISCONNECT:
//         mqtt_connected = false;
//         LOG_INF("Disconnected from MQTT broker");
//         break;

//     case MQTT_EVT_PUBLISH: {
//         const struct mqtt_publish_param *publish = &evt->param.publish;
//         LOG_INF("Message received on topic: %s", publish->message.topic.topic.utf8);

//         /* Handle messages */
//         if (strstr(publish->message.topic.topic.utf8, "start")) {
//             LOG_INF("Start command received");
//             /* Handle start command */
//         } else if (strstr(publish->message.topic.topic.utf8, "stop")) {
//             LOG_INF("Stop command received");
//             /* Handle stop command */
//         }
//         break;
//     }

//     case MQTT_EVT_SUBACK:
//         LOG_INF("Subscribed to topic(s)");
//         break;

//     case MQTT_EVT_PUBACK:
//         LOG_INF("Message published successfully");
//         break;

//     default:
//         break;
//     }
// }

// int mqtt_client_init_and_connect(void)
// {
//     int ret;

//     /* Resolve broker address */
//     struct sockaddr_in *broker = (struct sockaddr_in *)&broker_addr;
//     broker->sin_family = AF_INET;
//     broker->sin_port = htons(MQTT_BROKER_PORT);
//     ret = net_addr_pton(AF_INET, MQTT_BROKER_HOSTNAME, &broker->sin_addr);
//     if (ret != 0) {
//         LOG_ERR("Failed to resolve broker address, ret=%d", ret);
//         return ret;
//     }

//     /* Initialize MQTT client */
//     mqtt_client_init(&client_ctx);
//     client_ctx.broker = &broker_addr;
//     client_ctx.evt_cb = mqtt_event_handler;
//     client_ctx.client_id.utf8 = CLIENT_ID;
//     client_ctx.client_id.size = strlen(CLIENT_ID);
//     client_ctx.protocol_version = MQTT_VERSION_3_1_1;
//     client_ctx.transport.type = MQTT_TRANSPORT_NON_SECURE;

//     /* Assign buffers */
//     client_ctx.rx_buf = rx_buffer;
//     client_ctx.rx_buf_size = sizeof(rx_buffer);
//     client_ctx.tx_buf = tx_buffer;
//     client_ctx.tx_buf_size = sizeof(tx_buffer);

//     /* Connect to broker */
//     ret = mqtt_connect(&client_ctx);
//     if (ret != 0) {
//         LOG_ERR("MQTT connect failed, ret=%d", ret);
//         return ret;
//     }

//     LOG_INF("MQTT client initialized and connected");
//     return 0;
// }

// int mqtt_client_discovery_announce(const char *proplet_id, const char *channel_id)
// {
//     char topic[128];
//     snprintf(topic, sizeof(topic), DISCOVERY_TOPIC_TEMPLATE, channel_id);

//     char payload[128];
//     snprintf(payload, sizeof(payload),
//              "{\"proplet_id\":\"%s\",\"mg_channel_id\":\"%s\"}", proplet_id, channel_id);

//     struct mqtt_publish_param param = {
//         .message = {
//             .topic = {
//                 .topic = topic,
//                 .topic_len = strlen(topic),
//             },
//             .payload = {
//                 .data = payload,
//                 .len = strlen(payload),
//             },
//         },
//         .message_id = 0,
//         .dup_flag = 0,
//         .retain_flag = 0,
//         .qos = MQTT_QOS_1_AT_LEAST_ONCE,
//     };

//     int ret = mqtt_publish(&client_ctx, &param);
//     if (ret != 0) {
//         LOG_ERR("Failed to publish discovery announcement, ret=%d", ret);
//     }

//     return ret;
// }

// int mqtt_client_subscribe(const char *channel_id)
// {
//     char start_topic[128];
//     snprintf(start_topic, sizeof(start_topic), START_TOPIC_TEMPLATE, channel_id);

//     char stop_topic[128];
//     snprintf(stop_topic, sizeof(stop_topic), STOP_TOPIC_TEMPLATE, channel_id);

//     struct mqtt_topic topics[] = {
//         {
//             .topic = {
//                 .topic = start_topic,
//                 .topic_len = strlen(start_topic),
//             },
//             .qos = MQTT_QOS_1_AT_LEAST_ONCE,
//         },
//         {
//             .topic = {
//                 .topic = stop_topic,
//                 .topic_len = strlen(stop_topic),
//             },
//             .qos = MQTT_QOS_1_AT_LEAST_ONCE,
//         },
//     };

//     struct mqtt_subscription_list sub_list = {
//         .list = topics,
//         .list_count = ARRAY_SIZE(topics),
//         .message_id = 1,
//     };

//     int ret = mqtt_subscribe(&client_ctx, &sub_list);
//     if (ret != 0) {
//         LOG_ERR("Failed to subscribe to topics, ret=%d", ret);
//     }

//     return ret;
// }

// void mqtt_client_process(void)
// {
//     if (mqtt_connected) {
//         mqtt_input(&client_ctx);
//         mqtt_live(&client_ctx);
//     }
// }

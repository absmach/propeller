/*
 * client.c
 */

#include "client.h"

/* If you need the Wi-Fi header, you can include it here, e.g.:
 * #include <zephyr/net/wifi.h>
 */

/* Logging: create a separate log module for this client. */
LOG_MODULE_REGISTER(my_proplet_client, LOG_LEVEL_DBG);

/* Broker settings */
#define MQTT_BROKER_HOSTNAME       "test.mosquitto.org"
#define MQTT_BROKER_PORT           1883
#define MQTT_CLIENT_ID             "esp32s3_proplet"
#define MQTT_TOPIC_WASM_BINARY     "my_proplet/wasm_binary"
#define MQTT_TOPIC_EXEC_RESULT     "my_proplet/execution_result"

static uint8_t rx_buffer[2048];
static uint8_t tx_buffer[2048];

static struct mqtt_client client;
static struct sockaddr_storage broker_storage;

/* WAMR config */
#define WASM_MAX_APP_MEMORY      (64 * 1024)
#define WASM_STACK_SIZE          (8 * 1024)
#define WASM_HEAP_SIZE           (8 * 1024)

static void mqtt_evt_handler(struct mqtt_client *const c,
                             const struct mqtt_evt *evt);

static int mqtt_subscribe_to_topic(void);
static int publish_execution_result(const char *result_str);
static int execute_wasm_buffer(const uint8_t *wasm_buf, size_t wasm_size);

int prepare_broker(void)
{
    struct sockaddr_in *broker4 = (struct sockaddr_in *)&broker_storage;

    broker4->sin_family = AF_INET;
    broker4->sin_port = htons(MQTT_BROKER_PORT);

    if (net_addr_pton(AF_INET, MQTT_BROKER_HOSTNAME, &broker4->sin_addr)) {
        LOG_ERR("Failed to parse broker hostname/IP: %s", MQTT_BROKER_HOSTNAME);
        return -EINVAL;
    }

    return 0;
}

void init_mqtt_client(void)
{
    mqtt_client_init(&client);

    client.broker = &broker_storage;
    client.evt_cb = mqtt_evt_handler;

    client.client_id.utf8 = (uint8_t *)MQTT_CLIENT_ID;
    client.client_id.size = strlen(MQTT_CLIENT_ID);

    client.protocol_version = MQTT_VERSION_3_1_1;

    client.rx_buf = rx_buffer;
    client.rx_buf_size = sizeof(rx_buffer);
    client.tx_buf = tx_buffer;
    client.tx_buf_size = sizeof(tx_buffer);

    client.clean_session = 1;

    client.transport.type = MQTT_TRANSPORT_NON_SECURE;
}

int mqtt_connect_to_broker(void)
{
    int ret = mqtt_connect(&client);
    if (ret) {
        LOG_ERR("mqtt_connect failed: %d", ret);
        return ret;
    }
    LOG_INF("MQTT client connected");
    return 0;
}

void mqtt_process_events(void)
{
    mqtt_input(&client);
    mqtt_live(&client);
}

static int mqtt_subscribe_to_topic(void)
{
    struct mqtt_topic subscribe_topic = {
        .topic = {
            .utf8 = (uint8_t *)MQTT_TOPIC_WASM_BINARY,
            .size = strlen(MQTT_TOPIC_WASM_BINARY)
        },
        .qos = MQTT_QOS_1_AT_LEAST_ONCE
    };

    struct mqtt_subscription_list subscription_list = {
        .list = &subscribe_topic,
        .list_count = 1,
        .message_id = 1234
    };

    int ret = mqtt_subscribe(&client, &subscription_list);
    if (ret) {
        LOG_ERR("Failed to subscribe, err %d", ret);
        return ret;
    }

    LOG_INF("Subscribed to topic: %s", MQTT_TOPIC_WASM_BINARY);
    return 0;
}

static int publish_execution_result(const char *result_str)
{
    struct mqtt_publish_param param;
    struct mqtt_topic topic = {
        .topic = {
            .utf8 = (uint8_t *)MQTT_TOPIC_EXEC_RESULT,
            .size = strlen(MQTT_TOPIC_EXEC_RESULT),
        },
        .qos = MQTT_QOS_1_AT_LEAST_ONCE
    };

    param.message.topic = topic;
    param.message.payload.data = (uint8_t *)result_str;
    param.message.payload.len  = strlen(result_str);
    param.message_id = 5678;
    param.dup_flag = 0;
    param.retain_flag = 0;

    return mqtt_publish(&client, &param);
}

static int execute_wasm_buffer(const uint8_t *wasm_buf, size_t wasm_size)
{
    static bool wamr_initialized = false;
    if (!wamr_initialized) {
        RuntimeInitArgs init_args;
        memset(&init_args, 0, sizeof(RuntimeInitArgs));
        init_args.mem_alloc_type = Alloc_With_Pool;

        static uint8_t global_heap_buf[WASM_MAX_APP_MEMORY];
        init_args.mem_alloc_pool.heap_buf = global_heap_buf;
        init_args.mem_alloc_pool.heap_size = sizeof(global_heap_buf);

        if (!wasm_runtime_full_init(&init_args)) {
            LOG_ERR("Failed to initialize WAMR runtime");
            return -1;
        }

        wamr_initialized = true;
    }

    WASMModuleCommon *wasm_module = wasm_runtime_load((uint8_t*)wasm_buf,
                                                      (uint32_t)wasm_size,
                                                      NULL, 0);
    if (!wasm_module) {
        LOG_ERR("Failed to load WASM module");
        return -1;
    }

    WASMModuleInstanceCommon *wasm_module_inst =
        wasm_runtime_instantiate(wasm_module,
                                 WASM_STACK_SIZE,
                                 WASM_HEAP_SIZE,
                                 NULL, 0);
    if (!wasm_module_inst) {
        LOG_ERR("Failed to instantiate WASM module");
        wasm_runtime_unload(wasm_module);
        return -1;
    }

    const char *func_name = "add";
    wasm_function_inst_t func =
        wasm_runtime_lookup_function(wasm_module_inst, func_name, NULL);
    if (!func) {
        LOG_ERR("Exported function '%s' not found in wasm", func_name);
        goto clean;
    }

    uint32_t argv[2];
    argv[0] = 3;
    argv[1] = 4;

    if (!wasm_runtime_call_wasm(wasm_module_inst, func, 2, argv)) {
        LOG_ERR("Failed to call '%s'", func_name);
        goto clean;
    }

    int32_t result = (int32_t)argv[0];
    LOG_INF("WASM function '%s'(3,4) -> %d", func_name, result);

    char result_str[64];
    snprintf(result_str, sizeof(result_str),
             "Result from '%s'(3,4): %d", func_name, result);
    publish_execution_result(result_str);

clean:
    wasm_runtime_deinstantiate(wasm_module_inst);
    wasm_runtime_unload(wasm_module);
    return 0;
}

static void mqtt_evt_handler(struct mqtt_client *const c,
                             const struct mqtt_evt *evt)
{
    switch (evt->type) {
    case MQTT_EVT_CONNACK:
        LOG_INF("MQTT_EVT_CONNACK");
        mqtt_subscribe_to_topic();
        break;

    case MQTT_EVT_DISCONNECT:
        LOG_INF("MQTT_EVT_DISCONNECT");
        break;

    case MQTT_EVT_PUBLISH: {
        LOG_INF("MQTT_EVT_PUBLISH");

        const struct mqtt_publish_param *p = &evt->param.publish;

        LOG_INF("Topic: %.*s, payload len %d",
                p->message.topic.topic.size,
                (char *)p->message.topic.topic.utf8,
                p->message.payload.len);

        if (p->message.payload.len > sizeof(rx_buffer)) {
            LOG_ERR("Received payload is too large!");
            break;
        }

        int ret = mqtt_read_publish_payload(c, rx_buffer, p->message.payload.len);
        if (ret < 0) {
            LOG_ERR("mqtt_read_publish_payload error: %d", ret);
            break;
        }

        execute_wasm_buffer(rx_buffer, p->message.payload.len);

        struct mqtt_puback_param puback = {
            .message_id = p->message_id
        };
        mqtt_publish_qos1_ack(c, &puback);
        break;
    }

    case MQTT_EVT_PUBACK:
        LOG_INF("MQTT_EVT_PUBACK id=%u result=%d",
                evt->param.puback.message_id,
                evt->result);
        break;

    case MQTT_EVT_SUBACK:
        LOG_INF("MQTT_EVT_SUBACK");
        break;

    default:
        break;
    }
}

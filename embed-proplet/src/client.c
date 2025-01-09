#include <zephyr/kernel.h>
#include <zephyr/net/mqtt.h>
#include <zephyr/net/socket.h>
#include <zephyr/logging/log.h>
#include <zephyr/data/json.h>
#include <string.h>
#include <wasm_export.h>

LOG_MODULE_REGISTER(proplet_client, LOG_LEVEL_DBG);

#define MQTT_BROKER_HOSTNAME "test.mosquitto.org"
#define MQTT_BROKER_PORT     1883
#define MQTT_CLIENT_ID       "esp32s3_proplet"

#define CONTROL_TOPIC_TEMPLATE  "channels/%s/messages/control/manager"
#define REGISTRY_TOPIC_TEMPLATE "channels/%s/messages/registry/proplet"

static uint8_t rx_buffer[2048];
static uint8_t tx_buffer[2048];

#define WASM_MAX_APP_MEMORY      (64 * 1024)
#define WASM_STACK_SIZE          (8 * 1024)
#define WASM_HEAP_SIZE           (8 * 1024)

static struct {
    char channel_id[64];
    char thing_id[64];
    bool connected;
    uint8_t wasm_chunks[10][4096];
    size_t chunk_sizes[10];
    int total_chunks;
    int received_chunks;
    bool fetching_binary;
    char current_app[64];
} app_state;

struct json_rpc_request {
    char method[16];
    char params[128];
    int id;
};

static const struct json_obj_descr json_rpc_descr[] = {
    JSON_OBJ_DESCR_PRIM(struct json_rpc_request, method, JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM(struct json_rpc_request, params, JSON_TOK_STRING),
    JSON_OBJ_DESCR_PRIM(struct json_rpc_request, id, JSON_TOK_NUMBER),
};

static void start_wasm_app(const char *params);
static void stop_wasm_app(const char *params);
static void handle_json_rpc_message(const char *payload);
static void assemble_and_execute_wasm(void);
static void mqtt_evt_handler(struct mqtt_client *const c, const struct mqtt_evt *evt);

void init_mqtt_client(const char *channel_id, const char *thing_id)
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

    strncpy(app_state.channel_id, channel_id, sizeof(app_state.channel_id) - 1);
    strncpy(app_state.thing_id, thing_id, sizeof(app_state.thing_id) - 1);
    app_state.connected = false;
    app_state.total_chunks = 0;
    app_state.received_chunks = 0;
    app_state.fetching_binary = false;
    memset(app_state.current_app, 0, sizeof(app_state.current_app));
}

static void handle_json_rpc_message(const char *payload)
{
    struct json_rpc_request rpc = {0};

    if (json_obj_parse(payload, strlen(payload), json_rpc_descr,
                       ARRAY_SIZE(json_rpc_descr), &rpc) < 0) {
        LOG_ERR("Failed to parse JSON-RPC message");
        return;
    }

    LOG_INF("Received JSON-RPC method: %s, id: %d", rpc.method, rpc.id);

    if (strcmp(rpc.method, "start") == 0) {
        start_wasm_app(rpc.params);
    } else if (strcmp(rpc.method, "stop") == 0) {
        stop_wasm_app(rpc.params);
    } else {
        LOG_WRN("Unsupported JSON-RPC method: %s", rpc.method);
    }
}

static void start_wasm_app(const char *params)
{
    char app_name[64] = {0};

    sscanf(params, "[\"%63[^\"]", app_name);

    LOG_INF("Starting WASM app: %s", app_name);

    if (app_state.fetching_binary) {
        LOG_ERR("Already fetching another app, cannot fetch %s", app_name);
        return;
    }

    char topic[128];
    snprintf(topic, sizeof(topic), REGISTRY_TOPIC_TEMPLATE, app_state.channel_id);

    char payload[128];
    snprintf(payload, sizeof(payload), "{\"app_name\":\"%s\"}", app_name);

    struct mqtt_publish_param param = {
        .message.topic = {
            .topic = { .utf8 = topic, .size = strlen(topic) },
            .qos = MQTT_QOS_1_AT_LEAST_ONCE
        },
        .message.payload = { .data = payload, .len = strlen(payload) },
        .dup_flag = 0,
        .retain_flag = 0,
        .message_id = 1
    };

    int ret = mqtt_publish(&client, &param);
    if (ret != 0) {
        LOG_ERR("Failed to send registry request: %d", ret);
        return;
    }

    app_state.fetching_binary = true;
    strncpy(app_state.current_app, app_name, sizeof(app_state.current_app) - 1);

    LOG_INF("Requested WASM binary for app: %s", app_name);
}

static void stop_wasm_app(const char *params)
{
    char app_name[64] = {0};

    sscanf(params, "[\"%63[^\"]", app_name);

    LOG_INF("Stopping WASM app: %s", app_name);

    LOG_INF("WASM app %s stopped", app_name);
}

static void assemble_and_execute_wasm(void)
{
    size_t total_size = 0;
    for (int i = 0; i < app_state.received_chunks; i++) {
        total_size += app_state.chunk_sizes[i];
    }

    uint8_t *binary = k_malloc(total_size);
    if (!binary) {
        LOG_ERR("Failed to allocate memory for WASM binary");
        return;
    }

    size_t offset = 0;
    for (int i = 0; i < app_state.received_chunks; i++) {
        memcpy(binary + offset, app_state.wasm_chunks[i], app_state.chunk_sizes[i]);
        offset += app_state.chunk_sizes[i];
    }

    LOG_INF("Executing WASM binary for app: %s", app_state.current_app);

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
            k_free(binary);
            return;
        }
        wamr_initialized = true;
    }

    WASMModuleCommon *module = wasm_runtime_load(binary, total_size, NULL, 0);
    if (!module) {
        LOG_ERR("Failed to load WASM module");
        k_free(binary);
        return;
    }

    WASMModuleInstanceCommon *instance = wasm_runtime_instantiate(module, WASM_STACK_SIZE, WASM_HEAP_SIZE, NULL, 0);
    if (!instance) {
        LOG_ERR("Failed to instantiate WASM module");
        wasm_runtime_unload(module);
        k_free(binary);
        return;
    }

    const char *func_name = "run";
    wasm_function_inst_t func = wasm_runtime_lookup_function(instance, func_name, NULL);
    if (!func) {
        LOG_ERR("Function '%s' not found", func_name);
        goto cleanup;
    }

    uint32_t argv[2] = {3, 4};
    if (!wasm_runtime_call_wasm(instance, func, 2, argv)) {
        LOG_ERR("Failed to call '%s'", func_name);
        goto cleanup;
    }

    LOG_INF("Function '%s' executed successfully, result: %d", func_name, argv[0]);

cleanup:
    wasm_runtime_deinstantiate(instance);
    wasm_runtime_unload(module);
    k_free(binary);

    app_state.fetching_binary = false;
    app_state.total_chunks = 0;
    app_state.received_chunks = 0;
    memset(app_state.current_app, 0, sizeof(app_state.current_app));
}

static void mqtt_evt_handler(struct mqtt_client *const c, const struct mqtt_evt *evt)
{
    switch (evt->type) {
    case MQTT_EVT_CONNACK:
        LOG_INF("MQTT connected");
        app_state.connected = true;
        break;

   
    case MQTT_EVT_PUBLISH:
        if (evt->param.publish.message.payload.len < sizeof(rx_buffer)) {
            int ret = mqtt_read_publish_payload(c, rx_buffer, evt->param.publish.message.payload.len);
            if (ret < 0) {
                LOG_ERR("Failed to read payload: %d", ret);
                return;
            }
            rx_buffer[evt->param.publish.message.payload.len] = '\0'; 

            if (strcmp(evt->param.publish.message.topic.topic.utf8, 
                       "channels/<chan_id>/messages/control/manager") == 0) {
                handle_json_rpc_message(rx_buffer);
            } else if (strcmp(evt->param.publish.message.topic.topic.utf8, 
                              "channels/<chan_id>/messages/registry/proplet") == 0) {
                struct chunk_payload {
                    int chunk_idx;
                    int total_chunks;
                    uint8_t data[4096];
                };

                struct json_obj_descr chunk_payload_descr[] = {
                    JSON_OBJ_DESCR_PRIM(struct chunk_payload, chunk_idx, JSON_TOK_NUMBER),
                    JSON_OBJ_DESCR_PRIM(struct chunk_payload, total_chunks, JSON_TOK_NUMBER),
                    JSON_OBJ_DESCR_PRIM(struct chunk_payload, data, JSON_TOK_STRING),
                };

                struct chunk_payload chunk = {0};
                if (json_obj_parse(rx_buffer, strlen(rx_buffer), chunk_payload_descr,
                                   ARRAY_SIZE(chunk_payload_descr), &chunk) < 0) {
                    LOG_ERR("Failed to parse chunk payload");
                    return;
                }

                memcpy(app_state.wasm_chunks[chunk.chunk_idx], chunk.data, sizeof(chunk.data));
                app_state.chunk_sizes[chunk.chunk_idx] = strlen(chunk.data);
                app_state.total_chunks = chunk.total_chunks;
                app_state.received_chunks++;

                LOG_INF("Received chunk %d/%d", chunk.chunk_idx + 1, chunk.total_chunks);

                if (app_state.received_chunks == app_state.total_chunks) {
                    assemble_and_execute_wasm();
                }
            }
        } else {
            LOG_ERR("Received payload too large!");
        }
        break;

    case MQTT_EVT_DISCONNECT:
        LOG_INF("MQTT disconnected");
        app_state.connected = false;
        break;

    default:
        LOG_WRN("Unhandled MQTT event: %d", evt->type);
        break;
    }
}

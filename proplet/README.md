### **Topics Published By Proplet**

1. **On Creation (Startup Notification):**

   - **Topic:** `channels/$MANAGER_CHANNEL_ID/messages/control/proplet/create`
   - **Payload:**
     ```json
     {
       "PropletID": "{PropletID}",
       "ChanID": "{ChannelID}"
     }
     ```
   - The "create" topic is used to notify that a Proplet has come online, which is a one-time event.

2. **Liveliness Updates:**

   - **Topic:** `channels/$MANAGER_CHANNEL_ID/messages/control/proplet/alive`
   - **Interval:** Every 10 seconds.
   - **Payload:**
     ```json
     {
       "PropletID": "{PropletID}",
       "ChanID": "{ChannelID}"
     }
     ```
   - This enables the Manager to determine which Proplets are currently active and healthy by listening for these updates.

3. **Last Will & Testament (LWT):**
   - **Topic:** `channels/$MANAGER_CHANNEL_ID/messages/control/proplet/alive`
   - **Payload:**
     ```json
     {
       "status": "offline",
       "PropletID": "{PropletID}",
       "ChanID": "{ChannelID}"
     }
     ```
   - Triggered automatically by the MQTT broker when a Proplet goes offline.


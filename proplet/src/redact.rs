use serde_json::Value;

const REDACTED_MARKER: &str = "<REDACTED>";

const SENSITIVE_KEYS: [&str; 2] = ["file", "data"];

pub fn redact_value(value: &str) -> String {
    let chars: Vec<char> = value.chars().collect();
    if chars.len() <= 20 {
        return value.to_string();
    }

    let head: String = chars[..10].iter().collect();
    let tail: String = chars[chars.len() - 10..].iter().collect();

    format!("{head}{REDACTED_MARKER}{tail}")
}

pub fn redact_payload(payload: &str) -> String {
    match serde_json::from_str::<Value>(payload) {
        Ok(mut value) => {
            redact_json(&mut value);
            value.to_string()
        }
        Err(_) => payload.to_string(),
    }
}

fn redact_json(value: &mut Value) {
    match value {
        Value::Object(map) => {
            for (key, val) in map.iter_mut() {
                match val {
                    Value::String(s) if SENSITIVE_KEYS.contains(&key.as_str()) => {
                        *s = redact_value(s);
                    }
                    other => redact_json(other),
                }
            }
        }
        Value::Array(arr) => {
            for val in arr.iter_mut() {
                redact_json(val);
            }
        }
        _ => {}
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn short_values_are_unchanged() {
        assert_eq!(redact_value(""), "");
        assert_eq!(redact_value("short"), "short");
        assert_eq!(redact_value("01234567890123456789"), "01234567890123456789");
    }

    #[test]
    fn long_values_are_redacted() {
        let value = "0123456789ABCDEFGHIJ0123456789"; // 30 chars
        assert_eq!(redact_value(value), "0123456789<REDACTED>0123456789");
    }

    #[test]
    fn redacts_file_field_in_payload() {
        let file = "AGFzbQEAAAABBwFgAn9/AX8DAgEABwcBA2FkZAAA";
        let payload = json!({ "id": "task-1", "file": file }).to_string();

        let redacted = redact_payload(&payload);
        let value: Value = serde_json::from_str(&redacted).unwrap();

        assert_eq!(value["id"], "task-1");
        assert_eq!(value["file"], redact_value(file));
        assert_eq!(value["file"].as_str().unwrap().len(), 30);
        assert!(value["file"].as_str().unwrap().contains(REDACTED_MARKER));
        assert!(value["file"].as_str().unwrap().starts_with(&file[..10]));
        assert!(value["file"]
            .as_str()
            .unwrap()
            .ends_with(&file[file.len() - 10..]));
    }

    #[test]
    fn redacts_data_field_in_payload() {
        let data = "aGVsbG8gd29ybGQgdGhpcyBpcyBhIGNodW5r";
        let payload = json!({ "app_name": "my-app", "data": data }).to_string();

        let redacted = redact_payload(&payload);
        let value: Value = serde_json::from_str(&redacted).unwrap();

        assert_eq!(value["app_name"], "my-app");
        assert_eq!(value["data"], redact_value(data));
        assert!(value["data"].as_str().unwrap().contains(REDACTED_MARKER));
    }

    #[test]
    fn non_json_payload_is_unchanged() {
        assert_eq!(redact_payload("not json"), "not json");
    }
}

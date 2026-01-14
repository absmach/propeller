#![allow(dead_code)]

use thiserror::Error;

pub type Result<T> = std::result::Result<T, Error>;

#[derive(Debug, Error)]
pub enum Error {
    #[error("TEE detection error: {0}")]
    TeeDetection(String),

    #[error("TEE validation failed: {0}")]
    TeeValidation(String),

    #[error("Attestation error: {0}")]
    Attestation(String),

    #[error("KBS client error: {0}")]
    KbsClient(String),

    #[error("Secret parsing error: {0}")]
    SecretParse(String),

    #[error("Secret unsealing error: {0}")]
    SecretUnseal(String),

    #[error("Image handling error: {0}")]
    ImageHandling(String),

    #[error("Key unwrapping error: {0}")]
    KeyUnwrap(String),

    #[error("Resource not found: {0}")]
    ResourceNotFound(String),

    #[error("Configuration error: {0}")]
    Configuration(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("Serde error: {0}")]
    Serde(#[from] serde_json::Error),

    #[error("Base64 decode error: {0}")]
    Base64(#[from] base64::DecodeError),

    #[error("TOML parse error: {0}")]
    TomlParse(#[from] toml::de::Error),

    #[error("Crypto error: {0}")]
    Crypto(String),

    #[error("Unknown error: {0}")]
    Unknown(String),
}

#[cfg(feature = "cdh")]
impl From<kbs_protocol::Error> for Error {
    fn from(err: kbs_protocol::Error) -> Self {
        Error::KbsClient(err.to_string())
    }
}

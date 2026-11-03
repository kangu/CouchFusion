//! API errors: `{"error":{"code","message","fields?"}}` with the Go status mapping.

use axum::http::{HeaderValue, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde_json::{Map, Value, json};

#[derive(Debug, Clone)]
pub struct ApiError {
    pub code: &'static str,
    pub message: String,
    pub fields: Option<Map<String, Value>>,
}

impl ApiError {
    pub fn new(code: &'static str, message: impl Into<String>) -> Self {
        Self { code, message: message.into(), fields: None }
    }
    pub fn with_fields(mut self, fields: Map<String, Value>) -> Self {
        self.fields = Some(fields);
        self
    }
    pub fn validation(m: impl Into<String>) -> Self {
        Self::new("validation", m)
    }
    pub fn conflict(m: impl Into<String>) -> Self {
        Self::new("conflict", m)
    }
    pub fn not_found(m: impl Into<String>) -> Self {
        Self::new("not_found", m)
    }
    pub fn internal(m: impl Into<String>) -> Self {
        Self::new("internal", m)
    }
    pub fn filesystem(m: impl Into<String>) -> Self {
        Self::new("filesystem", m)
    }
    /// Server-side configuration problem the user must fix (422 `config`).
    pub fn config(m: impl Into<String>) -> Self {
        Self::new("config", m)
    }
    pub fn git(m: impl Into<String>) -> Self {
        Self::new("git", m)
    }
    pub fn unauthorized(m: impl Into<String>) -> Self {
        Self::new("unauthorized", m)
    }
    pub fn forbidden(m: impl Into<String>) -> Self {
        Self::new("forbidden", m)
    }

    pub fn status(&self) -> StatusCode {
        match self.code {
            "validation" | "config" => StatusCode::UNPROCESSABLE_ENTITY,
            "conflict" | "process" => StatusCode::CONFLICT,
            "not_found" => StatusCode::NOT_FOUND,
            "unavailable" => StatusCode::SERVICE_UNAVAILABLE,
            "git" => StatusCode::BAD_GATEWAY,
            "unauthorized" => StatusCode::UNAUTHORIZED,
            "forbidden" => StatusCode::FORBIDDEN,
            "unsupported_media_type" => StatusCode::UNSUPPORTED_MEDIA_TYPE,
            "request_too_large" => StatusCode::PAYLOAD_TOO_LARGE,
            "invalid_request" => StatusCode::BAD_REQUEST,
            _ => StatusCode::INTERNAL_SERVER_ERROR,
        }
    }

    pub fn payload(&self) -> crate::dto::ApiErrorPayload {
        crate::dto::ApiErrorPayload { code: self.code.to_string(), message: self.message.clone(), fields: self.fields.clone() }
    }
}

impl From<anyhow::Error> for ApiError {
    fn from(err: anyhow::Error) -> Self {
        tracing::error!("internal error: {err:#}");
        ApiError::internal("Internal server error.")
    }
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        let status = self.status();
        let mut body = json!({"code": self.code, "message": self.message});
        if let Some(fields) = self.fields {
            body["fields"] = Value::Object(fields);
        }
        let mut res = (status, axum::Json(json!({"error": body}))).into_response();
        res.headers_mut().insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
        res
    }
}

/// `{"data": …}` envelope with `Cache-Control: no-store`.
pub fn data<T: serde::Serialize>(status: StatusCode, value: T) -> Response {
    let mut res = (status, axum::Json(json!({"data": value}))).into_response();
    res.headers_mut().insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    res
}

pub type ApiResult = Result<Response, ApiError>;

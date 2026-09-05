package api

// Response is the standard API response wrapper
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
	Error   *ErrorBody  `json:"error,omitempty"`
}

// Meta contains pagination metadata
type Meta struct {
	Cursor  string `json:"cursor,omitempty"`
	HasMore bool   `json:"has_more"`
	Limit   int    `json:"limit,omitempty"`
}

// ErrorBody contains error details
type ErrorBody struct {
	Code      string      `json:"code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

// FieldError contains validation error for a specific field
type FieldError struct {
	Field      string      `json:"field"`
	Code       string      `json:"code"`
	Message    string      `json:"message"`
	Constraint interface{} `json:"constraint,omitempty"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status string            `json:"status" example:"healthy"`
	Checks map[string]string `json:"checks"`
}

// ReadyResponse represents readiness check response
type ReadyResponse struct {
	Status string `json:"status" example:"ready"`
}

// PingResponse represents ping response
type PingResponse struct {
	Message string `json:"message" example:"pong"`
}

package common

const ResponseCodeSuccess = 200

type Resp[T any] struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      T      `json:"data"`
	RequestID string `json:"request_id,omitempty"`
}

func NewResp[T any](code int, message string, data T, requestID string) Resp[T] {
	return Resp[T]{
		Code:      code,
		Message:   message,
		Data:      data,
		RequestID: requestID,
	}
}

type PageResp struct {
	Content interface{} `json:"content"`
	Total   int64       `json:"total"`
}

// Package httpapi 是控制面的 HTTP 基座（B0）：路由、中间件、统一响应与错误码映射。
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/roarzx/my-aladin/pkg/errcode"
)

type successBody struct {
	Data any `json:"data"`
}

// OK 写成功响应：{"data": ...}。
func OK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, data)
}

// JSON 写指定状态码的 JSON。
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Data 用统一成功信封包装。
func Data(w http.ResponseWriter, status int, data any) {
	JSON(w, status, successBody{Data: data})
}

// Fail 用统一错误体写响应。
func Fail(w http.ResponseWriter, err error) {
	errcode.Write(w, err)
}

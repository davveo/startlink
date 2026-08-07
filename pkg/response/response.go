package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/starlink/push/pkg/errcode"
)

type Body struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "ok", Data: data})
}

func Fail(c *gin.Context, err error) {
	if e, ok := err.(*errcode.Error); ok {
		status := http.StatusOK
		switch {
		case e.Code == 40101:
			status = http.StatusUnauthorized
		case e.Code == 40301:
			status = http.StatusForbidden
		case e.Code == 40401 || e.Code == 40402:
			status = http.StatusNotFound
		case e.Code == 40901 || e.Code == 40902 || e.Code == 40903:
			status = http.StatusConflict
		case e.Code == 42901:
			status = http.StatusTooManyRequests
		case e.Code >= 50000:
			status = http.StatusInternalServerError
		case e.Code >= 40000 && e.Code < 50000:
			status = http.StatusBadRequest
		}
		c.JSON(status, Body{Code: e.Code, Message: e.Message})
		return
	}
	c.Error(err) // 详细错误仅保留在服务端上下文，避免泄漏 SQL/网络等内部信息。
	c.JSON(http.StatusInternalServerError, Body{Code: errcode.Internal.Code, Message: errcode.Internal.Message})
}

package handler

import (
	"github.com/gin-gonic/gin"
)

// 统一业务错误码
//
// 响应格式: {"code": 0|错误码, "msg": "...", "data": ...}
//   - code == 0 表示成功,非 0 为具体业务错误码
//   - HTTP 状态码与业务码解耦:同一业务错误在不同场景可有不同 HTTP 状态码
//   - 前端只判断 code === 0 / code !== 0,升级为业务码不破坏兼容
type ErrCode int

const (
	// CodeOK 成功。成功分支沿用字面量 0(与既有响应一致),此处仅作文档性定义
	CodeOK              ErrCode = 0
	CodeInvalidParams   ErrCode = 1001 // 参数缺失、格式错误或不支持的操作
	CodeUnauthorized    ErrCode = 1002 // 未登录或登录状态无效
	CodeForbidden       ErrCode = 1003 // 已登录但无权操作该资源
	CodeNotFound        ErrCode = 1004 // 文件/资源不存在
	CodeUserExists      ErrCode = 1005 // 用户名已存在
	CodeInvalidCreds    ErrCode = 1006 // 用户名或密码错误
	CodeUploadFailed    ErrCode = 1007 // 上传失败
	CodeMergeFailed     ErrCode = 1008 // 分片合并失败
	CodeStorageError    ErrCode = 1009 // 存储层错误(如预签名仅支持 MinIO)
	CodeTooManyRequests ErrCode = 1010 // 请求过于频繁(限流)
	CodeSearchFailed    ErrCode = 1011 // AI 检索失败
	CodeInternalError   ErrCode = 1099 // 服务内部错误(兜底)
)

// respondError 统一错误响应:HTTP 状态码 + 业务错误码 + 可读消息
func respondError(c *gin.Context, httpStatus int, code ErrCode, msg string) {
	c.JSON(httpStatus, gin.H{"code": code, "msg": msg, "data": nil})
}

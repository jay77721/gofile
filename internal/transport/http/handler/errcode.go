package handler

import (
	"github.com/gin-gonic/gin"
)

// Unified business error codes
//
// Response format: {"code": 0|error code, "msg": "...", "data": ...}
//   - code == 0 means success; non-zero is a specific business error code
//   - HTTP status code is decoupled from business code: the same business error may map to different HTTP statuses
//   - Frontend only checks code === 0 / code !== 0; upgrading to business codes remains compatible
type ErrCode int

const (
	// CodeOK success. Successful branches keep literal 0 (consistent with existing responses); this is only a documentary definition
	CodeOK              ErrCode = 0
	CodeInvalidParams   ErrCode = 1001 // missing/invalid params or unsupported operation
	CodeUnauthorized    ErrCode = 1002 // not logged in or invalid session
	CodeForbidden       ErrCode = 1003 // authenticated but not authorized for this resource
	CodeNotFound        ErrCode = 1004 // file/resource not found
	CodeUserExists      ErrCode = 1005 // username already exists
	CodeInvalidCreds    ErrCode = 1006 // invalid username or password
	CodeUploadFailed    ErrCode = 1007 // upload failed
	CodeMergeFailed     ErrCode = 1008 // chunk merge failed
	CodeStorageError    ErrCode = 1009 // storage layer error (e.g., presigned URL only supports MinIO)
	CodeTooManyRequests ErrCode = 1010 // too many requests (rate limiting)
	CodeSearchFailed    ErrCode = 1011 // AI search failed
	CodeInternalError   ErrCode = 1099 // internal server error (fallback)
)

// respondError unified error response: HTTP status + business code + readable message
func respondError(c *gin.Context, httpStatus int, code ErrCode, msg string) {
	c.JSON(httpStatus, gin.H{"code": code, "msg": msg, "data": nil})
}

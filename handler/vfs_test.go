package handler

import (
	"bytes"
	"encoding/json"
	"gofile/model"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupVFSAuthRouter(fh *FileHandler, username string) *gin.Engine {
	r := setupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("username", username)
		c.Next()
	})
	r.POST("/file/folder/create", fh.CreateFolderHandler)
	r.POST("/file/folder/rename", fh.RenameFolderHandler)
	r.POST("/file/folder/move", fh.MoveFolderHandler)
	r.GET("/file/query", fh.FileQueryHandler)
	return r
}

func TestVFS_CreateFolderHandler(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupVFSAuthRouter(fh, "alice")

	t.Run("create root folder success", func(t *testing.T) {
		body, _ := json.Marshal(model.FolderCreateReq{Name: "Documents", ParentID: 0})
		req := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var resp struct {
			Code int            `json:"code"`
			Data model.UserFile `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Code != 0 || resp.Data.DirPath != "/Documents/" || resp.Data.IsDir != 1 || resp.Data.ID == 0 {
			t.Fatalf("unexpected create response: %+v", resp)
		}
	})

	t.Run("create subfolder success", func(t *testing.T) {
		// 先创建根目录
		body, _ := json.Marshal(model.FolderCreateReq{Name: "Projects", ParentID: 0})
		req := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var rootResp struct {
			Data model.UserFile `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &rootResp)
		rootID := rootResp.Data.ID

		// 创建子目录
		subBody, _ := json.Marshal(model.FolderCreateReq{Name: "Backend", ParentID: uint64(rootID)})
		subReq := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(subBody))
		subReq.Header.Set("Content-Type", "application/json")
		subW := httptest.NewRecorder()
		r.ServeHTTP(subW, subReq)

		if subW.Code != http.StatusOK {
			t.Fatalf("subfolder status = %d, want 200", subW.Code)
		}
		var subResp struct {
			Code int            `json:"code"`
			Data model.UserFile `json:"data"`
		}
		_ = json.Unmarshal(subW.Body.Bytes(), &subResp)
		if subResp.Data.DirPath != "/Projects/Backend/" || subResp.Data.ParentID != uint64(rootID) {
			t.Fatalf("unexpected subfolder response: %+v", subResp)
		}
	})

	t.Run("invalid folder names return 400", func(t *testing.T) {
		invalidCases := []string{"", "   ", "a/b", "a\\b", "..", "../escape"}
		for _, name := range invalidCases {
			body, _ := json.Marshal(model.FolderCreateReq{Name: name, ParentID: 0})
			req := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("name=%q status=%d, want 400", name, w.Code)
			}
		}
	})

	t.Run("invalid parent_id returns 400", func(t *testing.T) {
		body, _ := json.Marshal(model.FolderCreateReq{Name: "Lost", ParentID: 99999})
		req := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestVFS_RenameFolderHandler(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupVFSAuthRouter(fh, "alice")

	// 1. 创建文件夹
	body, _ := json.Marshal(model.FolderCreateReq{Name: "OldName", ParentID: 0})
	req := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var createResp struct {
		Data model.UserFile `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &createResp)
	folderID := createResp.Data.ID

	t.Run("rename folder success", func(t *testing.T) {
		renameBody, _ := json.Marshal(model.FolderRenameReq{FileID: folderID, NewName: "NewName"})
		renameReq := httptest.NewRequest("POST", "/file/folder/rename", bytes.NewReader(renameBody))
		renameReq.Header.Set("Content-Type", "application/json")
		renameW := httptest.NewRecorder()
		r.ServeHTTP(renameW, renameReq)

		if renameW.Code != http.StatusOK {
			t.Fatalf("rename status = %d, want 200", renameW.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(renameW.Body.Bytes(), &resp)
		if resp["code"].(float64) != 0 {
			t.Fatalf("rename code = %v, want 0", resp["code"])
		}
	})

	t.Run("invalid rename params return 400", func(t *testing.T) {
		// 空名称
		renameBody, _ := json.Marshal(model.FolderRenameReq{FileID: folderID, NewName: ""})
		renameReq := httptest.NewRequest("POST", "/file/folder/rename", bytes.NewReader(renameBody))
		renameReq.Header.Set("Content-Type", "application/json")
		renameW := httptest.NewRecorder()
		r.ServeHTTP(renameW, renameReq)
		if renameW.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", renameW.Code)
		}

		// 非法字符
		renameBody, _ = json.Marshal(model.FolderRenameReq{FileID: folderID, NewName: "bad/name"})
		renameReq = httptest.NewRequest("POST", "/file/folder/rename", bytes.NewReader(renameBody))
		renameReq.Header.Set("Content-Type", "application/json")
		renameW = httptest.NewRecorder()
		r.ServeHTTP(renameW, renameReq)
		if renameW.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", renameW.Code)
		}

		// 不存在的 file_id
		renameBody, _ = json.Marshal(model.FolderRenameReq{FileID: 99999, NewName: "valid"})
		renameReq = httptest.NewRequest("POST", "/file/folder/rename", bytes.NewReader(renameBody))
		renameReq.Header.Set("Content-Type", "application/json")
		renameW = httptest.NewRecorder()
		r.ServeHTTP(renameW, renameReq)
		if renameW.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", renameW.Code)
		}
	})
}

func TestVFS_MoveFolderHandler(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupVFSAuthRouter(fh, "alice")

	// 初始化文件夹结构：/FolderA/FolderB/ 与 /FolderTarget/
	bodyA, _ := json.Marshal(model.FolderCreateReq{Name: "FolderA", ParentID: 0})
	reqA := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(bodyA))
	reqA.Header.Set("Content-Type", "application/json")
	wA := httptest.NewRecorder()
	r.ServeHTTP(wA, reqA)
	var respA struct{ Data model.UserFile `json:"data"` }
	_ = json.Unmarshal(wA.Body.Bytes(), &respA)
	folderAID := respA.Data.ID

	bodyB, _ := json.Marshal(model.FolderCreateReq{Name: "FolderB", ParentID: uint64(folderAID)})
	reqB := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(bodyB))
	reqB.Header.Set("Content-Type", "application/json")
	wB := httptest.NewRecorder()
	r.ServeHTTP(wB, reqB)
	var respB struct{ Data model.UserFile `json:"data"` }
	_ = json.Unmarshal(wB.Body.Bytes(), &respB)
	folderBID := respB.Data.ID

	bodyTarget, _ := json.Marshal(model.FolderCreateReq{Name: "FolderTarget", ParentID: 0})
	reqTarget := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(bodyTarget))
	reqTarget.Header.Set("Content-Type", "application/json")
	wTarget := httptest.NewRecorder()
	r.ServeHTTP(wTarget, reqTarget)
	var respTarget struct{ Data model.UserFile `json:"data"` }
	_ = json.Unmarshal(wTarget.Body.Bytes(), &respTarget)
	targetID := respTarget.Data.ID

	t.Run("circular move prevention: moving FolderA into FolderB returns 400", func(t *testing.T) {
		moveBody, _ := json.Marshal(model.FolderMoveReq{FileID: folderAID, TargetParentID: uint64(folderBID)})
		moveReq := httptest.NewRequest("POST", "/file/folder/move", bytes.NewReader(moveBody))
		moveReq.Header.Set("Content-Type", "application/json")
		moveW := httptest.NewRecorder()
		r.ServeHTTP(moveW, moveReq)

		if moveW.Code != http.StatusBadRequest {
			t.Fatalf("circular move status = %d, want 400", moveW.Code)
		}
	})

	t.Run("valid move: moving FolderB to FolderTarget returns 200", func(t *testing.T) {
		moveBody, _ := json.Marshal(model.FolderMoveReq{FileID: folderBID, TargetParentID: uint64(targetID)})
		moveReq := httptest.NewRequest("POST", "/file/folder/move", bytes.NewReader(moveBody))
		moveReq.Header.Set("Content-Type", "application/json")
		moveW := httptest.NewRecorder()
		r.ServeHTTP(moveW, moveReq)

		if moveW.Code != http.StatusOK {
			t.Fatalf("valid move status = %d, want 200", moveW.Code)
		}
	})

	t.Run("move to non-existent target returns 400", func(t *testing.T) {
		moveBody, _ := json.Marshal(model.FolderMoveReq{FileID: folderAID, TargetParentID: 77777})
		moveReq := httptest.NewRequest("POST", "/file/folder/move", bytes.NewReader(moveBody))
		moveReq.Header.Set("Content-Type", "application/json")
		moveW := httptest.NewRecorder()
		r.ServeHTTP(moveW, moveReq)

		if moveW.Code != http.StatusBadRequest {
			t.Fatalf("move to non-existent target status = %d, want 400", moveW.Code)
		}
	})
}

func TestVFS_DirectoryQueryHandler(t *testing.T) {
	fh, _, _ := setupTestHandler(t)
	r := setupVFSAuthRouter(fh, "alice")

	// 1. 创建 /RootDocs/
	body, _ := json.Marshal(model.FolderCreateReq{Name: "RootDocs", ParentID: 0})
	req := httptest.NewRequest("POST", "/file/folder/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 2. 查询根目录 parent_id=0
	queryReq := httptest.NewRequest("GET", "/file/query?parent_id=0&page=1&size=20", nil)
	queryW := httptest.NewRecorder()
	r.ServeHTTP(queryW, queryReq)

	if queryW.Code != http.StatusOK {
		t.Fatalf("query status = %d, want 200", queryW.Code)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			List        []model.FileMeta   `json:"list"`
			Total       int64              `json:"total"`
			Breadcrumbs []model.Breadcrumb `json:"breadcrumbs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(queryW.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != 0 || resp.Data.Total != 1 || len(resp.Data.Breadcrumbs) == 0 {
		t.Fatalf("unexpected query response: %+v", resp)
	}
	if resp.Data.Breadcrumbs[0].Path != "/" {
		t.Errorf("expected root breadcrumb path '/', got %q", resp.Data.Breadcrumbs[0].Path)
	}
}

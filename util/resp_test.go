package util

import (
	"encoding/json"
	"testing"
)

func TestNewRespMsg(t *testing.T) {
	resp := NewRespMsg(0, "ok", map[string]string{"key": "value"})

	if resp.Code != 0 {
		t.Errorf("Code = %d, want 0", resp.Code)
	}
	if resp.Msg != "ok" {
		t.Errorf("Msg = %s, want ok", resp.Msg)
	}
}

func TestJSONBytes(t *testing.T) {
	resp := NewRespMsg(0, "success", nil)
	data := resp.JSONBytes()

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("JSONBytes() returned invalid JSON: %v", err)
	}

	if result["code"].(float64) != 0 {
		t.Errorf("code = %v, want 0", result["code"])
	}
	if result["msg"].(string) != "success" {
		t.Errorf("msg = %v, want success", result["msg"])
	}
}

func TestJSONString(t *testing.T) {
	resp := NewRespMsg(1, "error", "details")
	str := resp.JSONString()

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(str), &result); err != nil {
		t.Fatalf("JSONString() returned invalid JSON: %v", err)
	}

	if result["code"].(float64) != 1 {
		t.Errorf("code = %v, want 1", result["code"])
	}
}

func TestRespMsgWithNilData(t *testing.T) {
	resp := NewRespMsg(0, "ok", nil)
	data := resp.JSONBytes()

	var result map[string]interface{}
	json.Unmarshal(data, &result)

	// data 字段应该为 null
	if result["data"] != nil {
		t.Errorf("data = %v, want nil", result["data"])
	}
}

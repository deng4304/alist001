package mcp

import (
	"encoding/json"
	"time"

	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
)

type objJSON struct {
	Name     string            `json:"name"`
	Size     int64             `json:"size"`
	IsDir    bool              `json:"is_dir"`
	Modified time.Time         `json:"modified"`
	Created  time.Time         `json:"created"`
	HashInfo map[string]string `json:"hash_info,omitempty"`
}

func objToJSON(obj model.Obj) objJSON {
	j := objJSON{
		Name:     obj.GetName(),
		Size:     obj.GetSize(),
		IsDir:    obj.IsDir(),
		Modified: obj.ModTime(),
		Created:  obj.CreateTime(),
	}
	hi := obj.GetHash()
	if hm := hashInfoToMap(hi); len(hm) > 0 {
		j.HashInfo = hm
	}
	return j
}

func hashInfoToMap(hi utils.HashInfo) map[string]string {
	m := make(map[string]string)
	for ht, v := range hi.All() {
		if v != "" {
			m[ht.Name] = v
		}
	}
	return m
}

func jsonResult(v interface{}) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(data)), nil
}

func textResult(msg string) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(msg), nil
}

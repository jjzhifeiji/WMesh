package httpapi

import (
	"net/http"

	"wmesh/global/internal/service"
)

// 旧示教器文件导入，收入平台级。
func (h *Handler) mountLegacy(mux *http.ServeMux) {
	// 导入旧工艺和工程，收入平台级。
	mux.HandleFunc("POST /v1/legacy-import", h.importLegacy)
}

// 一份旧示教器文件，可单独覆盖或另起。
type legacyFileReq struct {
	Path      string `json:"path"`      // 相对路径
	Name      string `json:"name"`      // 显示名，可空
	Content   string `json:"content"`   // UTF-8 JSON
	Overwrite bool   `json:"overwrite"` // 本份同名改正文
	Rename    bool   `json:"rename"`    // 本份同名按路径另起
}

// 一批旧工艺和工程，缺路径的工程拒绝。
type legacyImportReq struct {
	Processes []legacyFileReq `json:"processes"` // 工艺文件
	Projects  []legacyFileReq `json:"projects"`  // 工程文件
	Overwrite bool            `json:"overwrite"` // 同名改正文保留身份；否就跳过
	Rename    bool            `json:"rename"`    // 同名按路径另起新名；覆盖优先
}

// WAN 管理员导入旧工艺/工程；缺路径的工程拒绝，已入工艺保留。
func (h *Handler) importLegacy(w http.ResponseWriter, r *http.Request) {
	// 接住请求体，解析失败再拒绝。
	var req legacyImportReq
	// 未知字段或坏 JSON 按坏请求拒绝。
	if err := decodeJSON(r, &req); err != nil {
		// 把不合法的原因回给调用方。
		writeBadRequest(w, err)
		return
	}
	// 先收成导入批次，再逐份填入文件。
	in := service.LegacyImport{
		Processes: make([]service.LegacyFile, 0, len(req.Processes)),
		Projects:  make([]service.LegacyFile, 0, len(req.Projects)),
		Overwrite: req.Overwrite,
		Rename:    req.Rename,
	}
	// 逐份收成工艺文件，缺路径由服务拒绝。
	for _, f := range req.Processes {
		// 收成一份旧文件，正文按字节保存。
		in.Processes = append(in.Processes, service.LegacyFile{Path: f.Path, Name: f.Name, Content: []byte(f.Content), Overwrite: f.Overwrite, Rename: f.Rename})
	}
	// 逐份收成工程文件，缺路径则整批拒绝。
	for _, f := range req.Projects {
		// 收成一份旧文件，正文按字节保存。
		in.Projects = append(in.Projects, service.LegacyFile{Path: f.Path, Name: f.Name, Content: []byte(f.Content), Overwrite: f.Overwrite, Rename: f.Rename})
	}
	// 导入旧文件到平台级，缺路径的工程拒绝。
	out, err := h.svc.Assets.ImportLegacy(r.Context(), bearer(r), in)
	// 没成功就按业务错误回写，不假装完成。
	if err != nil {
		// 按业务错误回写，内部细节不出网。
		writeErr(w, err)
		return
	}
	// 把结果交回调用方。
	writeJSON(w, http.StatusOK, out)
}

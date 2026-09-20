package httpapi

import (
	"net/http"

	"wmesh/factory/internal/service"
)

// 旧示教器文件导入。
func (h *Handler) mountLegacy(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/factories/{id}/legacy-import", h.importLegacy)
}

type legacyFileReq struct {
	Path      string `json:"path"`      // 相对路径
	Name      string `json:"name"`      // 显示名，可空
	Content   string `json:"content"`   // UTF-8 JSON
	Overwrite bool   `json:"overwrite"` // 本份同名改正文
	Rename    bool   `json:"rename"`    // 本份同名按路径另起
}

type legacyImportReq struct {
	Processes []legacyFileReq `json:"processes"` // 工艺文件
	Projects  []legacyFileReq `json:"projects"`  // 工程文件
	Overwrite bool            `json:"overwrite"` // 同名改正文保留身份；否就跳过
	Rename    bool            `json:"rename"`    // 同名按路径另起新名；覆盖优先
}

// 工艺工程师导入旧工艺/工程；缺路径的工程拒绝，已入工艺保留。
func (h *Handler) importLegacy(w http.ResponseWriter, r *http.Request) {
	h.withFactory(w, r, func(svc *service.Service) {
		var req legacyImportReq
		if err := decodeJSON(r, &req); err != nil {
			writeBadRequest(w, err)
			return
		}
		in := service.LegacyImport{
			Processes: make([]service.LegacyFile, 0, len(req.Processes)),
			Projects:  make([]service.LegacyFile, 0, len(req.Projects)),
			Overwrite: req.Overwrite,
			Rename:    req.Rename,
		}
		for _, f := range req.Processes {
			in.Processes = append(in.Processes, service.LegacyFile{Path: f.Path, Name: f.Name, Content: []byte(f.Content), Overwrite: f.Overwrite, Rename: f.Rename})
		}
		for _, f := range req.Projects {
			in.Projects = append(in.Projects, service.LegacyFile{Path: f.Path, Name: f.Name, Content: []byte(f.Content), Overwrite: f.Overwrite, Rename: f.Rename})
		}
		out, err := svc.Assets.ImportLegacy(r.Context(), bearer(r), in)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

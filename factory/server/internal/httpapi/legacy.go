package httpapi

import (
	"net/http"

	"wmesh/factory/internal/service"
)

// 旧示教器文件导入。
func (h *Handler) mountLegacy(mux *http.ServeMux) {
	// 超管导入旧工艺和工程，缺路径则拒绝。
	mux.HandleFunc("POST /v1/factories/{id}/legacy-import", h.importLegacy)
}

// 一份旧文件，可以单独覆盖或另起名字。
type legacyFileReq struct {
	Path      string `json:"path"`      // 相对路径
	Name      string `json:"name"`      // 显示名，可空
	Content   string `json:"content"`   // UTF-8 JSON
	Overwrite bool   `json:"overwrite"` // 本份同名改正文
	Rename    bool   `json:"rename"`    // 本份同名按路径另起
}

// 一批旧工艺和工程，以及同名时怎么处理。
type legacyImportReq struct {
	Processes []legacyFileReq `json:"processes"` // 工艺文件
	Projects  []legacyFileReq `json:"projects"`  // 工程文件
	Overwrite bool            `json:"overwrite"` // 同名改正文保留身份；否就跳过
	Rename    bool            `json:"rename"`    // 同名按路径另起新名；覆盖优先
}

// 工厂超管或整厂管理员导入旧工艺/工程；缺路径的工程拒绝，已入工艺保留。
func (h *Handler) importLegacy(w http.ResponseWriter, r *http.Request) {
	// 工厂超管或整厂管理员导入旧工艺/工程；缺路径的工程拒绝，已入工艺保留。
	h.withFactory(w, r, func(svc *service.Service) {
		// 承接一批旧文件和同名时的策略。
		var req legacyImportReq
		// 正文无法解析则拒绝，不进入业务。
		if err := decodeJSON(r, &req); err != nil {
			// 请求不合法，回非法请求并不进入业务。
			writeBadRequest(w, err)
			return
		}
		// 收成一批导入，同名可以覆盖或另起。
		in := service.LegacyImport{
			Processes: make([]service.LegacyFile, 0, len(req.Processes)),
			Projects:  make([]service.LegacyFile, 0, len(req.Projects)),
			Overwrite: req.Overwrite,
			Rename:    req.Rename,
		}
		// 逐份收进旧工艺，路径问题由业务拒绝。
		for _, f := range req.Processes {
			// 把文本收成字节再交给业务。
			in.Processes = append(in.Processes, service.LegacyFile{Path: f.Path, Name: f.Name, Content: []byte(f.Content), Overwrite: f.Overwrite, Rename: f.Rename})
		}
		// 逐份收进旧工程，缺路径则整批拒绝。
		for _, f := range req.Projects {
			// 把文本收成字节再交给业务。
			in.Projects = append(in.Projects, service.LegacyFile{Path: f.Path, Name: f.Name, Content: []byte(f.Content), Overwrite: f.Overwrite, Rename: f.Rename})
		}
		// 导入旧工艺和工程，缺路径的工程拒绝。
		out, err := svc.Assets.ImportLegacy(r.Context(), bearer(r), in)
		// 上一步没通过则停止，不把失败写成成功。
		if err != nil {
			// 把失败译成状态回给调用方，不写成功。
			writeErr(w, err)
			return
		}
		// 通过之后把结果回给调用方。
		writeJSON(w, http.StatusOK, out)
	})
}

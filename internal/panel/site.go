package panel

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

type siteSettings struct {
	Title string `json:"title"`
}

func (s *Service) getSiteSettings(w http.ResponseWriter, r *http.Request) {
	title, err := s.store.SiteTitle(r.Context())
	if err != nil {
		s.report(w, "读取网站标题失败", err)
		return
	}
	respond(w, http.StatusOK, siteSettings{Title: title})
}

func (s *Service) putSiteSettings(w http.ResponseWriter, r *http.Request) {
	var input siteSettings
	if !readJSON(w, r, &input) {
		return
	}
	title := strings.TrimSpace(input.Title)
	length := utf8.RuneCountInString(title)
	if length < 1 || length > 80 || strings.IndexFunc(title, unicode.IsControl) >= 0 {
		fail(w, http.StatusBadRequest, "网站标题需为 1–80 个字符，且不能包含控制字符")
		return
	}
	saved, err := s.store.UpdateSiteTitle(r.Context(), title)
	if err != nil {
		s.report(w, "保存网站标题失败", err)
		return
	}
	respond(w, http.StatusOK, siteSettings{Title: saved})
}

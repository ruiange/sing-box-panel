package panel

import (
	"net/http"

	qrcode "github.com/skip2/go-qrcode"
)

func (s *Service) subscriptionQR(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	switch format {
	case "", "clash", "v2ray", "shadowrocket", "sing-box":
	default:
		fail(w, http.StatusBadRequest, "不支持的订阅格式")
		return
	}
	if s.node == nil {
		fail(w, http.StatusServiceUnavailable, "尚未启用 sing-box 节点")
		return
	}
	s.mu.Lock()
	device, ok := s.device(w, r)
	s.mu.Unlock()
	if !ok {
		return
	}
	if !device.Enabled {
		fail(w, http.StatusConflict, "设备已停用")
		return
	}
	subscription := s.publicURL() + "/sub/" + device.SubscriptionToken
	if format != "" {
		subscription += "/" + format
	}
	image, err := qrcode.Encode(subscription, qrcode.Medium, 320)
	if err != nil {
		s.report(w, "生成订阅二维码失败", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(image)
}

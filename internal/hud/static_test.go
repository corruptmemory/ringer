package hud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticServesVendoredAssets(t *testing.T) {
	h := staticHandler()
	for _, tc := range []struct{ path, wantSub, wantCT string }{
		{"/static/vendor/htmx.min.js", "", "javascript"},
		{"/static/vendor/idiomorph.min.js", "", "javascript"},
		{"/static/ringside.css", ":root", "text/css"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.path, rec.Code)
		}
		if tc.wantSub != "" && !strings.Contains(rec.Body.String(), tc.wantSub) {
			t.Fatalf("%s: body missing %q", tc.path, tc.wantSub)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), tc.wantCT) {
			t.Fatalf("%s: content-type = %q, want ~%q", tc.path, rec.Header().Get("Content-Type"), tc.wantCT)
		}
	}
}

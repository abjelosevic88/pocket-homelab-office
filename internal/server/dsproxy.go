package server

// The Document Server through this app. A one-click install gives Pocket Office one address with a
// certificate (https://office.<box>) and none for the Document Server, and a browser on an https page
// can't load the editor from plain http://box:8311. So, unless ONLYOFFICE_URL names a real public
// address, browsers load it from BASE_URL/ds and this app passes those requests (websockets too) to
// ONLYOFFICE_INTERNAL_URL. X-Forwarded-Host carries the /ds prefix: that is how the Document Server is
// told it lives under a path (ONLYOFFICE's own nginx sample for a "virtual path" does the same).

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const dsPrefix = "/ds"

func (s *Server) withDSProxy(next http.Handler) http.Handler {
	if !s.cfg.DSProxied {
		return next
	}
	target, err1 := url.Parse(s.cfg.OnlyOfficeInternalURL)
	base, err2 := url.Parse(s.cfg.BaseURL)
	if err1 != nil || err2 != nil || target.Host == "" || base.Host == "" {
		return next
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			p := strings.TrimPrefix(pr.In.URL.Path, dsPrefix)
			if p == "" {
				p = "/"
			}
			pr.Out.URL.Path, pr.Out.URL.RawPath = p, ""
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.SetXForwarded() // X-Forwarded-For; Host and Proto are set below, from BASE_URL
			pr.Out.Header.Set("X-Forwarded-Host", base.Host+dsPrefix)
			pr.Out.Header.Set("X-Forwarded-Proto", base.Scheme)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Warn("document server", "path", r.URL.Path, "err", err)
			http.Error(w, "The Document Server isn't answering yet (it takes a few minutes to start).", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == dsPrefix || strings.HasPrefix(r.URL.Path, dsPrefix+"/") {
			rp.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

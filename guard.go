package main

import (
	"net"
	"net/http"
	"net/url"
)

// guard keeps other websites out.
//
// Sporeline listens only on this computer, but the browser you use it in
// also visits the rest of the internet. Without this, any page you
// happened to have open could quietly send a form to localhost and
// delete, restore or import things. Two checks:
//
//  1. The address in the browser must be localhost or 127.0.0.1. This
//     stops a trick where a website points its own name at your machine
//     and then reads your log as if it were its own page.
//  2. Anything that changes data must come from a Sporeline page. The
//     browser says where a form came from; if it came from elsewhere, it
//     is refused.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) {
			http.Error(w, "Sporeline only answers at localhost.", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !fromUs(r) {
			http.Error(w, "That request came from another website, so Sporeline ignored it. "+
				"If you clicked something inside Sporeline, reload the page and try again.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localHost(hostport string) bool {
	h := hostport
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		h = host
	}
	return h == "localhost" || h == "127.0.0.1"
}

func fromUs(r *http.Request) bool {
	// Modern browsers label every request with where it came from.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}
	// Older browsers: fall back to the Origin header when there is one.
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host != r.Host {
			return false
		}
	}
	return true
}

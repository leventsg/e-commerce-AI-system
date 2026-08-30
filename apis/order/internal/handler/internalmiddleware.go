package handler

import (
	"net"
	"net/http"

	"github.com/leventsg/e-commerce-AI-system/apis/order/internal/config"
)

func InternalCallMiddleware(c config.InternalCallConfig) func(http.HandlerFunc) http.HandlerFunc {
	matchers := parseAllowedInternalIPs(c.AllowedIPs)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ipText, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ipText = r.RemoteAddr
			}
			ip := net.ParseIP(ipText)
			if ip == nil || !internalIPAllowed(ip, matchers) {
				http.Error(w, "internal call forbidden", http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
}

type internalIPMatchers struct {
	ips  []net.IP
	nets []*net.IPNet
}

func parseAllowedInternalIPs(values []string) internalIPMatchers {
	var matchers internalIPMatchers
	for _, value := range values {
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			matchers.ips = append(matchers.ips, ip)
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			matchers.nets = append(matchers.nets, network)
		}
	}
	return matchers
}

func internalIPAllowed(ip net.IP, matchers internalIPMatchers) bool {
	for _, allowed := range matchers.ips {
		if allowed.Equal(ip) {
			return true
		}
	}
	for _, network := range matchers.nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

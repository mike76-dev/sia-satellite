package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/mike76-dev/sia-satellite/internal/build"
)

var dir string

func main() {
	log.Printf("satc v%v\n", build.ClientVersion)
	if build.GitRevision == "" {
		log.Println("WARN: compiled without build commit or version. To compile correctly, please use the makefile")
	} else {
		log.Println("Git Revision " + build.GitRevision)
	}

	flag.StringVar(&dir, "dir", ".", "Directory to store the config file and the logs")
	flag.Parse()
	cfg, err := loadConfig(dir)
	if err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	target, err := url.Parse(cfg.RenterdConfig.Address)
	if err != nil {
		log.Fatalf("Failed to parse target URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if strings.HasPrefix(req.URL.Path, "/api/") {
			log.Printf("Intercepted API call: %s %s", req.Method, req.URL.Path)
		}
	}

	log.Printf("satc listening on %s\n", cfg.APIConfig.Address)
	if err := http.ListenAndServe(cfg.APIConfig.Address, proxy); err != nil {
		log.Fatalf("Failed to start satc: %v", err)
	}
}

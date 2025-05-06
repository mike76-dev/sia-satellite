package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"

	api "github.com/mike76-dev/sia-satellite/api/public"
	"github.com/mike76-dev/sia-satellite/internal/build"
	"github.com/mike76-dev/sia-satellite/internal/utils"
	"github.com/mike76-dev/sia-satellite/persist"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var dir string
var satellite *api.Client
var logger *zap.Logger
var closeFn func()

func decodeRequest(body *bytes.Buffer, resp any) error {
	if err := json.NewDecoder(body).Decode(resp); err != nil {
		return utils.AddContext(err, "couldn't decode request body")
	}
	return nil
}

func main() {
	log.Printf("satc v%v\n", build.ClientVersion)
	if build.GitRevision == "" {
		log.Println("WARN: compiled without build commit or version. To compile correctly, please use the makefile")
	} else {
		log.Println("Git Revision " + build.GitRevision)
	}

	flag.StringVar(&dir, "dir", ".", "Directory to store the config file and the logs")
	flag.Parse()

	dir, err := filepath.Abs(dir)
	if err != nil {
		log.Fatalf("Provided parameter is invalid: %v\n", dir)
	}

	cfg, err := loadConfig(dir)
	if err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	logger, closeFn, err = persist.NewFileLogger(filepath.Join(dir, "satc.log"), zapcore.ErrorLevel)
	if err != nil {
		log.Fatalf("Couldn't initialize logger: %v\n", err)
	}
	defer closeFn()

	satellite = api.NewClient(cfg.SatelliteConfig.Address, cfg.SatelliteConfig.APIToken)

	target, err := url.Parse(cfg.RenterdConfig.Address)
	if err != nil {
		log.Fatalf("Failed to parse target URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if strings.HasPrefix(req.URL.Path, "/api/") {
			copyBuf := new(bytes.Buffer)
			if req.Body != nil {
				reader := bufio.NewReader(req.Body)
				tee := io.TeeReader(reader, copyBuf)
				pr, pw := io.Pipe()

				go func() {
					buf := make([]byte, 65536)
					for {
						n, err := tee.Read(buf)
						if n > 0 {
							_, wErr := pw.Write(buf[:n])
							if wErr != nil {
								log.Printf("Pipe write error: %v\n", wErr)
								break
							}
						}
						if err != nil {
							pw.CloseWithError(err)
							break
						}
					}
				}()

				req.Body = io.NopCloser(pr)
			}

			log.Printf("Intercepted API call: %s %s", req.Method, req.URL.Path)
			switch req.Method + " " + req.URL.Path {
			case "POST /bus/settings/gouging":
				if err := updateGougingSettings(copyBuf); err != nil {
					logger.Error("POST /bus/settings/gouging failed", zap.Error(err))
				}
			}
		}
	}

	log.Printf("satc listening on %s\n", cfg.APIConfig.Address)
	if err := http.ListenAndServe(cfg.APIConfig.Address, proxy); err != nil {
		log.Fatalf("Failed to start satc: %v", err)
	}
}

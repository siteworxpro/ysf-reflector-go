package main

import (
	"flag"
	"log"
	"os"

	"github.com/siteworxpro/ysf-reflector-go/internal/bridge"
	"github.com/siteworxpro/ysf-reflector-go/internal/config"
	"github.com/siteworxpro/ysf-reflector-go/internal/reflector"
)

var Version = "dev"

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Printf("error loading config %q: %v", *cfgPath, err)
		os.Exit(1)
	}

	log.Printf("Starting ysf-reflector-go version %s", Version)

	r := reflector.New(cfg)

	if len(cfg.Bridges) > 0 {
		bm := bridge.NewManager(cfg.Bridges, cfg.PaddedCallsign(), r)
		r.SetBridgeRelayer(bm)
		r.SetBridgeProvider(bm)
		bm.Start()
		defer bm.Stop()
	}

	if err := r.Run(); err != nil {
		log.Printf("reflector error: %v", err)
		os.Exit(1)
	}
}

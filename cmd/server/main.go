package main

import (
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/config"
	"0xbot.org/api/internal/contract"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/httpapi"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/mcpoauth"
	"0xbot.org/api/internal/mcpserver"
	"0xbot.org/api/internal/messaging"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/providers/evmrpc"
	"0xbot.org/api/internal/providers/lifi"
	"0xbot.org/api/internal/providers/llamaprices"
	"0xbot.org/api/internal/providers/padininbox"
	"0xbot.org/api/internal/providers/padinrooms"
	"0xbot.org/api/internal/rooms"
	"0xbot.org/api/internal/routing"
	"0xbot.org/api/internal/swaps"
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if e := run(); e != nil {
		slog.Error("server stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	c, e := config.Load()
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	db, e := postgres.Open(startup, c.DatabaseURL)
	cancel()
	if e != nil {
		return e
	}
	defer db.Close()
	ids := identity.Store{DB: db}
	settings := routing.Store{DB: db}
	records := operations.Store{DB: db}
	client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	swapProvider := lifi.New(c.LIFIKey, c.LIFIIntegrator)
	if c.LIFIKey == "" && c.LIFIPublicAccess {
		swapProvider = lifi.NewPublic(c.LIFIIntegrator)
	}
	router := routing.New([]data.Provider{
		llamaprices.New(),
		swapProvider,
		padininbox.Provider{DB: db},
		padinrooms.Provider{DB: db},
		evmrpc.New("publicnode", c.Publicnode, client), evmrpc.New("drpc", c.DRPC, client),
		evmrpc.NewForNetwork("publicnode-bsc", "https://bsc-rpc.publicnode.com", "eip155:56", client),
		evmrpc.NewForNetwork("bnbchain-bsc", "https://bsc-dataseed.bnbchain.org", "eip155:56", client),
		evmrpc.NewForNetwork("publicnode-polygon", "https://polygon-bor-rpc.publicnode.com", "eip155:137", client),
		evmrpc.NewForNetwork("drpc-polygon", "https://polygon.drpc.org", "eip155:137", client),
	}, settings, map[string]routing.Metadata{
		"lifi":               {Name: "LI.FI", Website: "https://li.fi", Network: "eip155:1", Networks: swaps.Networks()},
		"padin-rooms":        {Name: "PADIN Rooms", Network: "discussion", Networks: []string{}},
		"padin-inbox":        {Name: "PADIN Inbox", Network: "eip155:1", Networks: []string{"eip155:1"}},
		"defillama-prices":   {Name: "DefiLlama · prices", Website: "https://defillama.com", Network: "market", Networks: data.NetworkIDs()},
		"publicnode":         {Name: "PublicNode", Website: "https://www.publicnode.com"},
		"drpc":               {Name: "dRPC", Website: "https://drpc.org"},
		"publicnode-bsc":     {Name: "PublicNode · BSC", Website: "https://bsc.publicnode.com"},
		"bnbchain-bsc":       {Name: "BNB Chain · BSC", Website: "https://docs.bnbchain.org"},
		"publicnode-polygon": {Name: "PublicNode · Polygon", Website: "https://polygon.publicnode.com"},
		"drpc-polygon":       {Name: "dRPC · Polygon", Website: "https://drpc.org"},
	})
	service := operations.New(router, records, ids)
	api := &httpapi.API{Config: c, DB: db, Identity: ids, Keys: apikeys.Store{DB: db}, Operations: records, Service: service, Settings: settings, Router: router, Spec: contract.Spec}
	api.Messages = &messaging.Service{Provider: padininbox.Provider{DB: db}, Identity: ids, Limiter: ids, Availability: settings, Recorder: records}
	api.Rooms = &rooms.Service{Provider: padinrooms.Provider{DB: db}, Limiter: ids, Availability: settings, Recorder: records}
	api.MCP = mcpserver.NewWithRooms(service, func(ctx context.Context) string { return httpapi.WorkspaceFromContext(ctx) }, api.Messages, api.Rooms)
	oauth := mcpoauth.Store{DB: db, Resource: c.APIOrigin + "/mcp", Issuer: c.APIOrigin}
	api.OAuth = oauth
	srv := &http.Server{Addr: c.Addr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errs := make(chan error, 1)
	go func() { slog.Info("listening", "address", c.Addr); errs <- srv.ListenAndServe() }()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return srv.Shutdown(shutdown)
		case e := <-errs:
			if e == http.ErrServerClosed {
				return nil
			}
			return e
		case <-ticker.C:
			clean, cancel := context.WithTimeout(ctx, 5*time.Second)
			if e := ids.Cleanup(clean); e != nil {
				slog.Warn("expired auth cleanup failed")
			}
			if e := oauth.Cleanup(clean); e != nil {
				slog.Warn("expired MCP authorization cleanup failed")
			}
			cancel()
		}
	}
}

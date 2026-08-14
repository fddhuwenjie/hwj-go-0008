// Command doclock runs the document versioning and edit-lock HTTP service.
//
// It is a self-contained server with no external dependencies and no network
// access required to build. State is held in memory and is lost when the
// process exits.
//
// Usage:
//
//	doclock [-addr :8080] [-reap-interval 5s]
//
// The server listens on the given address (default :8080) and shuts down
// gracefully on SIGINT or SIGTERM. A background reaper periodically removes
// expired edit locks.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fddhuwenjie/hwj-go-0008/internal/domain"
	"github.com/fddhuwenjie/hwj-go-0008/internal/httpapi"
	"github.com/fddhuwenjie/hwj-go-0008/internal/service"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	reapInterval := flag.Duration("reap-interval", 5*time.Second, "interval for reaping expired locks (0 disables)")
	flag.Parse()

	svc := service.New(service.Options{Clock: domain.SystemClock{}})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *reapInterval > 0 {
		svc.StartReaper(ctx, *reapInterval)
	}

	srv := httpapi.New(svc)
	log.Printf("doclock: listening on %s", *addr)
	if err := srv.Run(ctx, *addr); err != nil {
		log.Fatalf("doclock: server error: %v", err)
	}
	log.Println("doclock: shut down cleanly")
}

// Command pango is the whole product: one static binary that opens one
// SQLite file and serves the API and the site.
//
// There is no config file, no service dependency and no bootstrap step. That is
// the deployment story (§1) — a tablet, a laptop, an office NAS or a Raspberry
// Pi is a complete installation — and it only stays true if the binary keeps
// needing nothing but a path to write to.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vul-os/pango/backend/internal/api"
	"github.com/vul-os/pango/backend/internal/repo"
	"github.com/vul-os/pango/backend/internal/store"
	"github.com/vul-os/pango/backend/internal/sync"
	"github.com/vul-os/pango/backend/internal/sync/substrate"
	"github.com/vul-os/pango/backend/internal/wrap"
)

// version is the build's version string, overridable at link time.
var version = "0.1.0"

// envOr reads an environment variable, falling back to def when it is unset or
// blank. Used for settings docs/CONFIGURATION.md publishes under an env name and
// that a flag may also override.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		dbPath  = flag.String("db", "pango.db", "path to the SQLite database file")
		addr    = flag.String("addr", "127.0.0.1:8099", "listen address")
		demo    = flag.Bool("demo", false, "run an ephemeral in-memory instance seeded with demo data")
		origins = flag.String("origins", "", "comma-separated CORS allowlist (default: same-origin only)")
		secure  = flag.Bool("secure-cookies", false, "mark session cookies Secure (set when served over HTTPS)")

		// Peer sync and WRAP (§7, §8) are both off by default: a fresh
		// install talks to nothing (§11), and enrolling into a mesh or
		// speaking to another organisation is a decision an operator makes
		// explicitly, not something that starts happening on upgrade.
		syncListen = flag.Bool("sync-listen", false, "serve /api/sync/* so other nodes can pull from and push to this one")
		syncPeer   = flag.String("sync-peer", "", "comma-separated peer base URLs to sync with on an interval")
		syncFolder = flag.String("sync-folder", "", "shared folder path for file-transport sync (a synced drive, a NAS mount, a USB stick)")
		wrapFlag   = flag.Bool("wrap", false, "enable the WRAP trades/v0 binding (github.com/vul-os/wrap) for cross-organisation work")

		// The merge engine is a DEPLOYMENT-WIDE switch, never a gradual
		// rollout (docs/SYNC.md §10, docs/CONFIGURATION.md). Two engines with
		// different total orders cannot share a replica set and the failure is
		// silent divergence rather than an error, so every node of a mesh must
		// be given the same value here before any of them syncs.
		// The default comes from PANGO_MERGER because that is the name
		// docs/CONFIGURATION.md already publishes for this setting; the flag
		// overrides it. Unlike the pairing secret this is not sensitive, so
		// having it on the command line is fine.
		mergeEngine = flag.String("merge-engine", envOr("PANGO_MERGER", "builtin"),
			"merge authority: `builtin` (Pango's own HLC engine) or `substrate` / `dmtap` "+
				"(the shared DMTAP-SYNC algebra, github.com/vul-os/kotva). Must be the same "+
				"on every node of a replica set")
	)
	flag.Parse()

	// The default listen address is loopback, not 0.0.0.0. A fresh install
	// talks to nothing and is reachable from nothing (§11); exposing it to a
	// network is a decision someone makes explicitly.
	if err := run(*dbPath, *addr, *origins, *demo, *secure, *syncListen, *syncPeer, *syncFolder,
		*wrapFlag, *mergeEngine); err != nil {
		log.Fatalf("pango: %v", err)
	}
}

func run(dbPath, addr, origins string, demo, secureCookies, syncListen bool, syncPeer, syncFolder string,
	wrapEnabled bool, mergeEngine string) error {
	if demo {
		// Demo data must never land in a real database. In-memory means the
		// dataset cannot outlive the process or overwrite anything on disk.
		dbPath = ":memory:"
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	// The merge authority is chosen HERE, before a single write is accepted
	// (docs/SYNC.md §10). It cannot be switched later in the process's life and
	// it must not differ between nodes of a replica set: two engines with
	// different notions of element identity cannot share one, and the failure is
	// silent divergence rather than an error.
	//
	// An unrecognised value is fatal rather than defaulted. Defaulting would
	// mean a typo in a deployment script silently ran the other algebra, which
	// is exactly the mistake that cannot be detected afterwards.
	switch mergeEngine {
	case "builtin":
		// Pango's own HLC engine. store.Merger stays nil.
	case "substrate", "dmtap": // "dmtap" is the spelling docs/CONFIGURATION.md publishes.
		sub, err := substrate.Open(context.Background(), st, substrate.Options{
			// Persisting wazero's compiled code turns a few hundred
			// milliseconds of start-up into a few tens. Optional: with no cache
			// directory the engine simply compiles on every boot.
			CacheDir: os.Getenv("PANGO_WASM_CACHE"),
		})
		if err != nil {
			return fmt.Errorf("merge engine: %w", err)
		}
		defer sub.Close(context.Background())
		st.SetMerger(sub)
		v, err := sub.Version()
		if err != nil {
			return fmt.Errorf("merge engine version: %w", err)
		}
		log.Printf("pango: merge engine: %s (binding %s, engine %s, substrate %s, "+
			"skew bound %dms, drift bound above it %s)",
			substrate.EngineName, v.Binding, v.Engine, v.Substrate, v.HLCSkewMS, sub.MaxDrift())
	default:
		return fmt.Errorf(
			"unknown merge engine %q: expected \"builtin\" or \"substrate\" (alias \"dmtap\"). "+
				"Refusing to default, because a node that quietly ran the other algebra would "+
				"diverge from its peers with no error anywhere", mergeEngine)
	}

	r := repo.New(st)
	if err := r.PurgeExpiredSessions(); err != nil {
		log.Printf("pango: purge expired sessions: %v", err)
	}

	srv := api.New(r, version)
	srv.SecureCookies = secureCookies
	srv.Demo = demo
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			srv.AllowedOrigins = append(srv.AllowedOrigins, o)
		}
	}

	if demo {
		creds, err := seedDemo(r)
		if err != nil {
			return fmt.Errorf("seed demo: %w", err)
		}
		log.Printf("pango: DEMO MODE — in-memory database, nothing is saved")
		log.Printf("pango: sign in as %s / %s", creds.Email, creds.Password)
	}

	mux := buildMux(srv, wrapEnabled, st.PublicKeyHex())

	// Peer sync (§7): off unless an operator asks for it, either by serving
	// requests, dialing peers, or pointing at a shared folder. The pairing
	// secret is read from the environment rather than a flag, never argv
	// (§11): a process listing on a shared box must not leak it.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	if syncListen || syncPeer != "" || syncFolder != "" {
		syncEngine := sync.New(st)
		syncEngine.SecretFn = func() string { return os.Getenv("PANGO_SYNC_SECRET") }
		syncEngine.AllowSecretFallback = os.Getenv("PANGO_SYNC_SECRET_FALLBACK") == "1"
		if syncFolder != "" {
			syncEngine.FolderFn = func() string { return syncFolder }
		}

		if syncListen {
			// More specific than "/api/", so it is matched first for
			// /api/sync/* without shadowing the rest of the API (§9 layering:
			// sync is its own package, not routed through api/).
			mux.Handle("/api/sync/", syncEngine.Handler())
			log.Printf("pango: sync: serving /api/sync/* (node key %s)", syncEngine.NodeID())
		}
		if syncPeer != "" || syncFolder != "" {
			const syncInterval = 60 * time.Second
			go syncEngine.RunBackground(bgCtx, syncInterval, func() []string {
				return store.SplitList(syncPeer)
			})
			log.Printf("pango: sync: background round every %s (peers=%q folder=%q)",
				syncInterval, syncPeer, syncFolder)
		}
	}

	if wrapEnabled {
		log.Printf("pango: wrap: trades/v0 binding enabled (identity %s)", st.PublicKeyHex())
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("pango %s listening on http://%s", version, addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Graceful shutdown matters more here than in a clustered service: this
	// process is the only copy. A hard kill mid-write on a Raspberry Pi with a
	// cheap SD card is how a maintenance history gets truncated.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case <-stop:
		log.Printf("pango: shutting down")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpSrv.Shutdown(ctx)
}

// buildMux assembles the routes run serves: the API, the optional site and
// app bundles, the .well-known namespace, and — when enabled — the WRAP
// identity route. Split out from run so a test can drive real requests
// through the exact routing table production uses without opening a socket.
func buildMux(srv *api.Server, wrapEnabled bool, nodePubKeyHex string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Handler())

	// The marketing/docs site is optional. A build without it simply does not
	// register the route rather than failing to start — the API is the
	// product, the site is a convenience.
	if site := newSiteHandler(); site != nil {
		mux.Handle("/site/", http.StripPrefix("/site/", site))
	} else {
		log.Printf("pango: no site/ directory found; /site/ not served")
	}

	// The app itself. Registered last and at the root so it catches everything
	// the API and the site did not: /api/ and /site/ are more specific patterns
	// and win, so mounting here cannot shadow them.
	if app := newAppHandler(); app != nil {
		mux.Handle("/", app)
	} else {
		log.Printf("pango: no web/dist/ found; the app is not served (run `npm run build` in web/)")
	}

	// /.well-known/* is a namespace other software probes to discover this
	// node — the WRAP identity route below is one such path, registered only
	// when --wrap is on. Without this catch-all, an unregistered .well-known
	// path (WRAP off, or any path WRAP does not define) falls through to the
	// SPA handler above, which — like any client-routed app — serves
	// index.html with a 200 for a path it does not recognise. That is correct
	// for /jobs/… but wrong here: a prober asking whether this node speaks a
	// well-known protocol should get an honest 404, not HTML dressed as
	// success. ServeMux matches the more specific "/.well-known/wrap/identity"
	// pattern first when it is registered, so registering this general
	// catch-all does not shadow it — Go's mux prefers the longer, more
	// specific pattern regardless of registration order.
	mux.HandleFunc("/.well-known/", http.NotFound)

	// WRAP (§8): the trades/v0 binding. Off by default — in-house
	// maintenance never touches it. The one endpoint wired at this layer is
	// the spec's own unauthenticated identity announcement
	// (github.com/vul-os/wrap 10-transport.md §11.1.1): a public key is not
	// sensitive, and a peer WRAP node needs to discover it before anything
	// else — offers, bids and assignments — can happen. Everything past
	// that (pool client, offer/assignment handling) is designed but not
	// wired here; see docs/WRAP.md's implementation-status table.
	if wrapEnabled {
		mux.HandleFunc("GET /.well-known/wrap/identity", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"pubkey":%q,"v":%d,"profiles":[%q]}`,
				nodePubKeyHex, wrap.FormatVersion, wrap.ProfileTrades)
		})
	}

	return mux
}

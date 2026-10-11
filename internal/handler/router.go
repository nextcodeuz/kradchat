package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kradchat/internal/config"
	"kradchat/internal/middleware"
	"kradchat/internal/security"
	"kradchat/internal/service"
	"kradchat/internal/ws"
)

type Router struct {
	cfg *config.Config
	svc *service.Services
	hub *ws.Hub

	origins     []string
	authHandler *AuthHandler
	uploadH     *UploadHandler
	bgH         *BackgroundHandler
	blockH      *BlockHandler
	wsH         *WSHandler
	rateLimiter *security.RateLimiter
	rateGC      *middleware.PeriodicGC
	wsStop      chan struct{}
}

func NewRouter(cfg *config.Config, svc *service.Services, hub *ws.Hub) *Router {
	limiter := security.NewRateLimiter(
		float64(cfg.Security.RateLimitReq),
		cfg.Security.RateLimitWindow,
	)
	origins := []string{
		"http://localhost:" + strconv.Itoa(cfg.Server.Port),
		"http://127.0.0.1:" + strconv.Itoa(cfg.Server.Port),
	}
	middleware.SetTrustedProxies(cfg.Security.TrustedProxies)
	return &Router{
		cfg:         cfg,
		svc:         svc,
		hub:         hub,
		origins:     origins,
		authHandler: NewAuthHandler(svc, hub),
		uploadH:     NewUploadHandler(svc),
		bgH:         NewBackgroundHandler(svc),
		blockH:      NewBlockHandler(svc),
		wsH:         NewWSHandler(cfg, svc, hub, svc.Auth, middleware.AllowedOrigin(origins)),
		rateLimiter: limiter,
		rateGC:      middleware.NewPeriodicGC(limiter, cfg.Security.RateLimitWindow),
		wsStop:      make(chan struct{}),
	}
}

func (rt *Router) StartGC() {
	rt.rateGC.Start()
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				rt.wsH.SweepLimiters()
			case <-rt.wsStop:
				return
			}
		}
	}()
}
func (rt *Router) StopGC() {
	rt.rateGC.Stop()
	select {
	case <-rt.wsStop:
	default:
		close(rt.wsStop)
	}
}

func (rt *Router) Handler() http.Handler {
	mux := http.NewServeMux()

	// === Public ===
	mux.HandleFunc("POST /api/auth/register", limitBody(16*1024)(rt.authHandler.Register))
	mux.HandleFunc("POST /api/auth/login", limitBody(16*1024)(rt.authHandler.Login))
	mux.HandleFunc("POST /api/auth/twofa/verify", limitBody(16*1024)(rt.authHandler.TwoFAVerify))
	mux.HandleFunc("POST /api/auth/refresh", limitBody(16*1024)(rt.authHandler.Refresh))

	// === Protected ===
	protected := middleware.Auth(rt.svc.Auth)
	authed := http.NewServeMux()
	authed.HandleFunc("POST /api/auth/logout", limitBody(16*1024)(rt.authHandler.Logout))
	authed.HandleFunc("POST /api/auth/password", limitBody(16*1024)(rt.authHandler.Password))
	authed.HandleFunc("POST /api/auth/twofa/setup", limitBody(16*1024)(rt.authHandler.TwoFASetup))
	authed.HandleFunc("POST /api/auth/twofa/enable", limitBody(16*1024)(rt.authHandler.TwoFAEnable))
	authed.HandleFunc("POST /api/auth/twofa/disable", limitBody(16*1024)(rt.authHandler.TwoFADisable))
	authed.HandleFunc("GET /api/me", rt.authHandler.Me)
	authed.HandleFunc("PATCH /api/me", limitBody(16*1024)(rt.authHandler.UpdateMe))
	authed.HandleFunc("POST /api/me/avatar", limitBody(6*1024*1024)(rt.authHandler.MeAvatar))
	authed.HandleFunc("GET /api/users/search", rt.authHandler.SearchUser)
	authed.HandleFunc("GET /api/conversations", rt.authHandler.Conversations)
	authed.HandleFunc("POST /api/users/{id}/block", rt.blockH.Block)
	authed.HandleFunc("DELETE /api/users/{id}/block", rt.blockH.Unblock)
	authed.HandleFunc("GET /api/devices", rt.authHandler.Devices)
	authed.HandleFunc("DELETE /api/devices/{id}", rt.authHandler.TerminateDevice)
	authed.HandleFunc("POST /api/upload", limitBody(3*1024*1024)(rt.uploadH.Upload))
	authed.HandleFunc("GET /api/upload/{id}/status", rt.uploadH.Status)
	authed.HandleFunc("DELETE /api/upload/{id}", rt.uploadH.Cancel)
	authed.HandleFunc("GET /api/files", rt.uploadH.ListFiles)
	authed.HandleFunc("DELETE /api/files/{id}", rt.uploadH.DeleteFile)
	authed.HandleFunc("PATCH /api/files/{id}", limitBody(16*1024)(rt.uploadH.RenameFile))
	authed.HandleFunc("PATCH /api/files/by-url", limitBody(16*1024)(rt.uploadH.RenameFileByURL))
	authed.HandleFunc("POST /api/files/save", limitBody(16*1024)(rt.uploadH.SaveFileToMy))
	authed.HandleFunc("GET /api/backgrounds", rt.bgH.List)
	authed.HandleFunc("POST /api/backgrounds/set", limitBody(16*1024)(rt.bgH.Set))
	mux.Handle("/api/", protected(authed))

	// === WebSocket ===
	mux.HandleFunc("/ws", rt.wsH.Serve)

	// === Static + index ===
	staticDir := rt.cfg.StaticDir
	wasmDir := filepath.Join(filepath.Dir(staticDir), "wasm")
	// uploads/tmp — yarim qolgan (to'liq merge qilinmagan) fayllar xizmat qilmaydi.
	// noListDir: katalog listing 404 (fayl nomlari sanab olinmaydi — enumeration oldi).
	// staticPolicy: uploads/ immutable keshlanadi (UUID nomlar) + noindex;
	// qolgan static no-cache (CSS/WASM yangilanishi tez ko'rinadi).
	// uploadsDisposition: PDF/ZIP kabi fayllar brauzerda emas, yuklab olinadi
	// (inline ochilishi phishing/xavfli kontent ko'rinishi oldini oladi).
	mux.Handle("/static/", middleware.GzipStatic(staticPolicy(uploadsDisposition(http.StripPrefix("/static/", http.FileServer(noListDir(blockTmpDir(http.Dir(staticDir)))))))))
	// wasm har safar tekshiriladi — eski keshlangan app.wasm bug'larni keltirardi.
	// GzipStatic bilan ~3x kichik keladi (5MB → ~1.7MB).
	mux.Handle("/wasm/", middleware.GzipStatic(http.StripPrefix("/wasm/", noCache(http.FileServer(http.Dir(wasmDir))))))
	mux.HandleFunc("/", serveIndex(staticDir))

	// === Middleware chain ===
	var chain http.Handler = mux
	chain = middleware.SecurityHeaders(chain)
	chain = middleware.CORS(middleware.AllowedOrigin(rt.origins))(chain)
	chain = middleware.RateLimit(rt.rateLimiter)(chain)
	chain = middleware.Logging(chain)
	return chain
}

// limitBody — request body'ni belgilangan hajm bilan cheklaydi.
func limitBody(n int64) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next(w, r)
		}
	}
}

// blockTmpDir — /static/uploads/tmp papkasini 404 qiladi.
func blockTmpDir(fs http.FileSystem) http.FileSystem {
	return &blockingFS{fs: fs}
}

// noCache — brauzerga faylni har doim qayta tekshirishni buyuradi
// (eski keshlangan app.wasm muammolarini oldini oladi).
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// noListDir — katalog so'rovlarini 404 qiladi (FileServer directory listing
// o'chiradi — upload fayl nomlarini sanab bo'lmaydi).
func noListDir(fs http.FileSystem) http.FileSystem {
	return &noListFS{fs: fs}
}

type noListFS struct {
	fs http.FileSystem
}

func (n *noListFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if st.IsDir() {
		_ = f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// staticPolicy — uploads/ uchun immutable kesh + noindex; qolganiga no-cache.
func staticPolicy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/static/"), "uploads/") {
			// UUID nomli fayllar o'zgarmas — 1 yil keshlanadi.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

type blockingFS struct {
	fs http.FileSystem
}

func (b *blockingFS) Open(name string) (http.File, error) {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "uploads/tmp" || strings.HasPrefix(clean, "uploads/tmp/") {
		return nil, os.ErrNotExist
	}
	return b.fs.Open(name)
}

// uploadsDisposition — foydalanuvchi yuklagan xavfli kengaytmalarni brauzer
// ichida ochish o'rniga yuklab olishni majburlaydi (phishing/XSS manzarasida
// inline ko'rinish xavfi). Rasmlar/video/audio odatdagidek inline ko'rinadi.
func uploadsDisposition(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/uploads/") {
			switch strings.ToLower(filepath.Ext(r.URL.Path)) {
			case ".pdf", ".zip", ".docx", ".xlsx", ".txt":
				w.Header().Set("Content-Disposition", "attachment")
			}
		}
		next.ServeHTTP(w, r)
	})
}

func serveIndex(staticDir string) http.HandlerFunc {
	indexPath := filepath.Join(filepath.Dir(staticDir), "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// API/WS/static/wasm — o'z handler'lari; SPA route'lar (/#/...) index'ga.
		if p == "/" || (!strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/ws") &&
			!strings.HasPrefix(p, "/static/") && !strings.HasPrefix(p, "/wasm/")) {
			http.ServeFile(w, r, indexPath)
			return
		}
		http.NotFound(w, r)
	}
}

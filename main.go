package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed assets
var assets embed.FS

//go:embed tmpl
var tmplFS embed.FS

const version = "1.5"

var store *Store

// dataDir puts your log somewhere you can find it and back it up.
//
//	Windows: %USERPROFILE%\sporeline
//	macOS:   ~/Library/Application Support/sporeline
//	Linux:   $XDG_DATA_HOME/sporeline or ~/.local/share/sporeline
func dataDir() (string, error) {
	if runtime.GOOS == "windows" {
		if up := os.Getenv("USERPROFILE"); up != "" {
			return filepath.Join(up, "sporeline"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "sporeline"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "sporeline"), nil
	}
	return filepath.Join(home, ".local", "share", "sporeline"), nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// listen takes the first free port from the preferred one upward. If a
// busy port turns out to be another copy of Sporeline, we say so rather
// than starting a second one quietly against the same files — a second
// window pointing at the same data is how you end up looking at a stale
// page and wondering why nothing changed.
func listen(pref int) (net.Listener, int, bool, error) {
	for p := pref; p < pref+20; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			return l, p, false, nil
		}
		if alreadyRunning(p) {
			return nil, p, true, nil
		}
	}
	return nil, 0, false, fmt.Errorf("no free port between %d and %d", pref, pref+20)
}

// alreadyRunning asks whoever holds the port whether they are Sporeline.
func alreadyRunning(port int) bool {
	client := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/alive", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	return strings.HasPrefix(string(body), "sporeline")
}

func main() {
	headless := flag.Bool("headless", false, "do not open a browser window")
	port := flag.Int("port", 8099, "preferred port")
	dir := flag.String("data", "", "data directory (defaults to your home folder)")
	flag.Parse()

	d := *dir
	if d == "" {
		var err error
		if d, err = dataDir(); err != nil {
			fatal("Could not work out where to keep your data: %v", err)
		}
	}
	var err error
	if store, err = OpenStore(d); err != nil {
		fatal("Could not open your log: %v", err)
	}
	if err := loadTemplates(); err != nil {
		fatal("Could not load templates: %v", err)
	}

	mux := http.NewServeMux()
	assetFS, _ := fs.Sub(assets, "assets")
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetFS))))
	mux.Handle("GET /pics/", http.StripPrefix("/pics/", http.FileServer(http.Dir(store.PicsDir()))))

	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		b, err := assets.ReadFile("assets/sporeline.ico")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(b)
	})
	mux.HandleFunc("GET /alive", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "sporeline %s", version)
	})
	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("GET /list/{kind}", handleList)
	mux.HandleFunc("GET /component/{id}", handleComponent)
	mux.HandleFunc("GET /component/{id}/edit", handleComponentEdit)
	mux.HandleFunc("POST /component/{id}", handleComponentUpdate)
	mux.HandleFunc("POST /component/{id}/gone", handleGone)
	mux.HandleFunc("POST /component/{id}/delete", handleDelete)
	mux.HandleFunc("POST /component/{id}/pics", handleAddPics)
	mux.HandleFunc("POST /component/{id}/pics/delete", handleDeletePic)
	mux.HandleFunc("GET /add", handleAddForm)
	mux.HandleFunc("POST /add", handleAdd)
	mux.HandleFunc("GET /recipes", handleRecipes)
	mux.HandleFunc("GET /recipe/{id}", handleRecipe)
	mux.HandleFunc("GET /recipe-form", handleRecipeForm)
	mux.HandleFunc("POST /recipe", handleRecipeSave)
	mux.HandleFunc("POST /recipe/{id}/delete", handleRecipeDelete)
	mux.HandleFunc("GET /data", handleDataPage)
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		render(w, "stats.html", page{Title: "Statistics", Tab: "stats", Data: buildStats()})
	})
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/data", http.StatusMovedPermanently)
	})
	mux.HandleFunc("POST /settings", handleSettingsSave)
	mux.HandleFunc("POST /backup/now", handleBackupNow)
	mux.HandleFunc("POST /backup/restore", handleBackupRestore)
	mux.HandleFunc("GET /backup/{name}", handleBackupDownload)
	mux.HandleFunc("GET /export", handleExport)
	mux.HandleFunc("POST /import", handleImportAny)
	mux.HandleFunc("GET /guide", handleGuide)

	store.BackupDaily()
	store.CleanRemovedPics()

	l, actual, running, err := listen(*port)
	if err != nil {
		fatal("%v", err)
	}
	url := fmt.Sprintf("http://localhost:%d/", actual)
	if running {
		fmt.Printf("\n  Sporeline is already running.\n")
		fmt.Printf("  Opening the copy at %s\n", url)
		fmt.Printf("  Close its window first if you meant to restart it.\n\n")
		if !*headless {
			openBrowser(url)
		}
		return
	}
	fmt.Printf("\n  Sporeline %s\n", version)
	fmt.Printf("  Your log lives in: %s\n", store.Dir())
	fmt.Printf("  Open %s in your browser.\n", url)
	fmt.Printf("  Leave this window open while you work. Close it to stop.\n\n")
	if !*headless {
		go func() { time.Sleep(300 * time.Millisecond); openBrowser(url) }()
	}
	log.Fatal(http.Serve(l, guard(mux)))
}

func fatal(format string, args ...any) {
	fmt.Printf("\n  Sporeline could not start.\n  "+format+"\n\n  Press Enter to close.\n", args...)
	fmt.Scanln()
	os.Exit(1)
}

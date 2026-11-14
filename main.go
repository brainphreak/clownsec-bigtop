// Clownsec Big Top: a small media server that streams your moflex library to the Clownsec 3DS
// video player. Run it, add your video folders in the page it opens, and press BIG TOP on the 3DS.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/catalog"
	"github.com/brainphreak/clownsec-bigtop/internal/config"
	"github.com/brainphreak/clownsec-bigtop/internal/discovery"
	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/poster"
	"github.com/brainphreak/clownsec-bigtop/internal/scan"
	"github.com/brainphreak/clownsec-bigtop/internal/server"
)

var version = "dev" // set at build time: -ldflags "-X main.version=..."

func main() {
	dataFlag := flag.String("data", "", "folder for Big Top's settings, library and posters (default: the user's app-data folder)")
	portFlag := flag.Int("port", 0, "HTTP port (default 32500, or the last one set)")
	noBrowser := flag.Bool("no-browser", false, "don't open the setup page on start")
	flag.Parse()

	fmt.Printf("Clownsec Big Top %s\n", version)
	dir, err := config.DataDir(*dataFlag)
	if err != nil {
		fatal("data folder: %v", err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fatal("settings: %v", err)
	}
	if *portFlag > 0 {
		cfg.Update(func(c *config.Config) { c.Port = *portFlag })
	}
	store, err := library.OpenStore(dir)
	if err != nil {
		fatal("library: %v", err)
	}
	posters, err := poster.Open(dir)
	if err != nil {
		fatal("posters: %v", err)
	}
	cat := catalog.New(library.Norm)
	if err := cat.Load(dir); err != nil {
		log.Printf("Clownsec catalog not available yet (%v); matching by file name", err)
	}
	sc := &scan.Scanner{Store: store, Catalog: cat, Posters: posters, DataDir: dir}
	srv := &server.Server{Cfg: cfg, Store: store, Scanner: sc, Posters: posters, Catalog: cat, Version: version}

	port := cfg.Snapshot().Port
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		fatal("port %d is in use -- is Big Top already running? (%v)", port, err)
	}
	go func() {
		err := discovery.Serve(config.DiscoveryPort, func() discovery.Answer {
			c := cfg.Snapshot()
			return discovery.Answer{Service: "clownsec-bigtop", Name: c.Name, ID: c.ID, Port: c.Port, Version: version}
		})
		log.Printf("discovery stopped (the 3DS can still connect by address): %v", err)
	}()

	page := fmt.Sprintf("http://localhost:%d/", port)
	fmt.Printf("Data folder: %s\nSetup page:  %s\n", dir, page)
	fmt.Println("Keep this window open while you watch. Close it to stop Big Top.")

	// look for new and changed files now and every half hour (unchanged files cost a stat each)
	go func() {
		for {
			sc.Start(false)
			time.Sleep(30 * time.Minute)
		}
	}()
	if !*noBrowser {
		go func() { time.Sleep(500 * time.Millisecond); openBrowser(page) }()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		store.Save()
		os.Exit(0)
	}()
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 20 * time.Second}
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("server: %v", err)
	}
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	cmd.Start()
}

// fatal explains and waits: on Windows the window would otherwise vanish before it can be read.
func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "\nBig Top can't start: "+f+"\n", a...)
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "Press Enter to close.")
		fmt.Scanln()
	}
	os.Exit(1)
}

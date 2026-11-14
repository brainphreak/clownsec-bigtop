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
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/brainphreak/clownsec-bigtop/internal/catalog"
	"github.com/brainphreak/clownsec-bigtop/internal/config"
	"github.com/brainphreak/clownsec-bigtop/internal/discovery"
	"github.com/brainphreak/clownsec-bigtop/internal/library"
	"github.com/brainphreak/clownsec-bigtop/internal/poster"
	"github.com/brainphreak/clownsec-bigtop/internal/scan"
	"github.com/brainphreak/clownsec-bigtop/internal/server"
	"github.com/brainphreak/clownsec-bigtop/internal/update"
)

var version = "dev" // set at build time: -ldflags "-X main.version=..."

func main() {
	dataFlag := flag.String("data", "", "folder for Big Top's settings, library and posters (default: the user's app-data folder)")
	portFlag := flag.Int("port", 0, "HTTP port (default 32500, or the last one set)")
	noBrowser := flag.Bool("no-browser", false, "don't open the setup page on start")
	replaces := flag.String("replaces", "", "") // set by the updater: the old build to wait for and remove
	flag.Parse()
	fixConsole()

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
	upd := update.New(version)
	srv := &server.Server{Cfg: cfg, Store: store, Scanner: sc, Posters: posters, Catalog: cat, Version: version, Updater: upd}
	srv.Quit = func() { store.Save(); os.Exit(0) }
	srv.Restart = func(newPath string) error {
		// the new build, with this run's options (and no second browser tab: the page reconnects)
		var args []string
		skip := false
		for _, a := range os.Args[1:] {
			if skip {
				skip = false
				continue
			}
			if a == "-replaces" || a == "--replaces" {
				skip = true
				continue
			}
			if strings.HasPrefix(a, "-replaces=") || strings.HasPrefix(a, "--replaces=") || a == "-no-browser" || a == "--no-browser" {
				continue
			}
			args = append(args, a)
		}
		self, _ := update.Self()
		args = append(args, "-no-browser", "-replaces", self)
		store.Save()
		fmt.Printf("\n  Updating: starting %s ...\n\n", filepath.Base(newPath))
		if err := spawn(newPath, args, dir); err != nil {
			fmt.Printf("  The new version didn't start (%v). This one keeps running.\n\n", err)
			return err
		}
		go func() { time.Sleep(400 * time.Millisecond); os.Exit(0) }()
		return nil
	}

	port := cfg.Snapshot().Port
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	for i := 0; err != nil && *replaces != "" && i < 40; i++ { // the old build is still letting go
		time.Sleep(500 * time.Millisecond)
		ln, err = net.Listen("tcp", ":"+strconv.Itoa(port))
	}
	if err != nil {
		fatal("port %d is in use -- is Big Top already running? (%v)", port, err)
	}
	if *replaces != "" {
		go update.Cleanup(*replaces)
	}
	if version != "dev" {
		go upd.Loop(func(st update.Status) {
			log.Printf("Big Top %s is available -- open the setup page to update.", st.Latest)
		})
	}
	go func() {
		err := discovery.Serve(config.DiscoveryPort, func() discovery.Answer {
			c := cfg.Snapshot()
			return discovery.Answer{Service: "clownsec-bigtop", Name: c.Name, ID: c.ID, Port: c.Port, Version: version}
		})
		log.Printf("discovery stopped (the 3DS can still connect by address): %v", err)
	}()

	page := fmt.Sprintf("http://localhost:%d/", port)
	// say plainly that it's up: after an update this is a fresh window nobody opened by hand
	fmt.Printf("\n  Big Top %s is running.\n\n", version)
	fmt.Printf("  Setup page:  %s\n", page)
	for _, a := range lanAddrs() {
		if port != config.DefaultPort {
			a += ":" + strconv.Itoa(port)
		}
		fmt.Printf("  On the 3DS:  BIG TOP > Find servers, or enter %s\n", a)
	}
	fmt.Printf("  Data folder: %s\n\n", dir)
	fmt.Println("  Keep this window open while you watch. Close it to stop Big Top.")
	fmt.Println()
	setTitle("Clownsec Big Top " + version + " - running")

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

// fatal explains and waits: on Windows the window would otherwise vanish before it can be read.
func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "\nBig Top can't start: "+f+"\n", a...)
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "Press Enter to close.")
		fmt.Scanln()
	}
	os.Exit(1)
}

// the addresses a 3DS can be given (the same list the setup page shows)
func lanAddrs() []string {
	var out []string
	ifs, _ := net.InterfaceAddrs()
	for _, a := range ifs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
			out = append(out, n.IP.String())
		}
	}
	return out
}

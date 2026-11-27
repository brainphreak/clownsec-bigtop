package library

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// BIGTOP_TVLIST=<file of paths> go test -run TestTVList ./internal/library -v
func TestTVList(t *testing.T) {
	fn := os.Getenv("BIGTOP_TVLIST")
	if fn == "" {
		t.Skip("BIGTOP_TVLIST not set")
	}
	f, err := os.Open(fn)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, _ := os.Create(fn + ".parsed.tsv")
	defer out.Close()
	shows := map[string]int{}
	noNum, n := 0, 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "\ufeff")
		if p == "" {
			continue
		}
		n++
		r := ParseTV(p, []string{`Z:\TV`})
		shows[r.Title]++
		if r.Episode == 0 {
			noNum++
		}
		fmt.Fprintf(out, "%s\t%d\tS%02dE%02d\t%s\t%s\n", r.Title, r.Year, r.Season, r.Episode, r.EpTitle, p)
	}
	type kv struct {
		k string
		v int
	}
	var l []kv
	for k, v := range shows {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].k < l[j].k })
	sf, _ := os.Create(fn + ".shows.txt")
	for _, x := range l {
		fmt.Fprintf(sf, "%5d %s\n", x.v, x.k)
	}
	sf.Close()
	t.Logf("%d files, %d shows, %d without an episode number", n, len(shows), noNum)
}
